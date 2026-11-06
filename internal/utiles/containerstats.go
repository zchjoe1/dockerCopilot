package utiles

// 【本地新增 2026-10-07】容器资源占用（CPU / 内存）采样。
//
// 为什么是「后台采样 + 内存缓存」，而不是在接口里现取：
//
//	① Docker 的 CPU% 本质是【两次采样计数器之差】。单次快照只能算出
//	   「容器启动至今的平均占用」，把它当实时值显示是错的 ——
//	   一个刚跑完编译的容器会长时间挂着很高的 CPU%，看着像出问题了。
//	   SDK 里的 ContainerStatsOneShot（?one-shot=1）正是这种单次快照。
//	② /api/containers 被前端每 10 秒轮询一次（bundle 里 refetchInterval:1e4）。
//	   若每次请求都现取，23 个容器各要等一个采样周期，列表接口会被拖慢 1 秒以上，
//	   而且请求越频繁、对 Docker daemon 的压力越大。
//
// 所以：后台 goroutine 每 StatsInterval 采一轮，算完存内存；
// 接口只读缓存（零延迟、零额外开销），数字跟着前端自己的轮询自动刷新。
// 这也正是飞牛自己监控面板的做法（实测连续调用 8~9ms 返回、数值会变，
// 说明是后台采样 + 内存缓存，不是每次现采）。

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

// StatsInterval 采样间隔。前端 10 秒轮询一次列表，5 秒保证每次拿到的都是新样本。
// 想改频率只改这一行即可（调大更省，调小更实时）。
const StatsInterval = 5 * time.Second

// ContainerStats 是单个容器的资源占用快照，字段会直接嵌进 /api/containers 的返回。
type ContainerStats struct {
	// CPUPercent 相对【单核】的百分比：100 表示占满一个核，200 表示占满两个核。
	CPUPercent float64 `json:"cpuPercent"`
	// MemUsed 已用内存（已扣除 page cache），字节。
	MemUsed uint64 `json:"memUsed"`
	// MemLimit 内存上限，字节。未设置上限时为 0，前端据此不显示 "/ 上限"。
	MemLimit uint64 `json:"memLimit"`
	// MemPercent 内存占用率（相对上限）。未设上限时为 0。
	MemPercent float64 `json:"memPercent"`
	// SampledAt 采样时刻（Unix 秒），便于调用方判断数据新鲜度。
	SampledAt int64 `json:"sampledAt"`
}

// cpuSample 是算 CPU% 所需的原始累计计数器。
type cpuSample struct {
	total     uint64  // cpu_stats.cpu_usage.total_usage
	system    uint64  // cpu_stats.system_cpu_usage
	onlineCPU float64 // cpu_stats.online_cpus
}

type statsStore struct {
	mu   sync.RWMutex
	cur  map[string]ContainerStats // 对外可读的最新结果
	prev map[string]cpuSample      // 上一轮计数器，用于做差
}

var statsCache = &statsStore{
	cur:  make(map[string]ContainerStats),
	prev: make(map[string]cpuSample),
}

// GetContainerStats 读缓存，不阻塞、不发起任何 Docker 调用。
// 第二个返回值为 false 表示该容器没有可用数据（已停止 / 刚启动还没采到 / 采样失败），
// 调用方应把对应字段留空，而不是填 0。
func GetContainerStats(id string) (ContainerStats, bool) {
	statsCache.mu.RLock()
	defer statsCache.mu.RUnlock()
	s, ok := statsCache.cur[id]
	return s, ok
}

// StartStatsSampler 起后台采样循环，随进程常驻，不需要停止。
func StartStatsSampler(svcCtx *svc.ServiceContext) {
	go func() {
		for {
			sampleContainerStats(svcCtx)
			time.Sleep(StatsInterval)
		}
	}()
}

// sampleContainerStats 采一轮：并行读所有运行中容器，然后一次性换掉缓存。
func sampleContainerStats(svcCtx *svc.ServiceContext) {
	if svcCtx == nil || svcCtx.DockerClient == nil {
		return
	}
	// 单轮总预算：即使某个容器卡住，也不能让下一轮叠上来。
	ctx, cancel := context.WithTimeout(context.Background(), StatsInterval*2)
	defer cancel()

	// All:false —— 只要运行中的，已停止的容器本来就没有 stats。
	list, err := svcCtx.DockerClient.ContainerList(ctx, container.ListOptions{All: false})
	if err != nil {
		logx.Errorf("资源采样：取容器列表失败: %v", err)
		return
	}

	type sampleResult struct {
		id  string
		raw cpuSample
		st  ContainerStats
		ok  bool
	}
	// 带缓冲：所有 goroutine 都能立刻写入，不会因为没人收而泄漏。
	results := make(chan sampleResult, len(list))
	var wg sync.WaitGroup
	for _, c := range list {
		if c.State != "running" {
			continue
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			r, err := readContainerStats(ctx, svcCtx, id)
			if err != nil {
				results <- sampleResult{id: id}
				return
			}
			results <- sampleResult{id: id, raw: r.raw, st: r.st, ok: true}
		}(c.ID)
	}
	wg.Wait()
	close(results)

	now := time.Now().Unix()
	statsCache.mu.Lock()
	defer statsCache.mu.Unlock()

	// 每轮重建：容器停了或删了，它的旧数字必须跟着消失。
	next := make(map[string]ContainerStats, len(list))
	for r := range results {
		if !r.ok {
			delete(statsCache.prev, r.id)
			continue
		}
		st := r.st
		st.SampledAt = now
		// CPU% = (本轮容器 CPU 增量 / 本轮系统 CPU 增量) × 核数 × 100。
		// 没有上一轮（进程刚起、或容器刚启动）时先只存基线，本轮不出 CPU 数字，
		// 5 秒后的下一轮就正常了 —— 内存是瞬时值，本轮即可用。
		if prev, ok := statsCache.prev[r.id]; ok && r.raw.system > prev.system {
			cpuDelta := float64(r.raw.total) - float64(prev.total)
			sysDelta := float64(r.raw.system) - float64(prev.system)
			if cpuDelta >= 0 && sysDelta > 0 {
				cores := r.raw.onlineCPU
				if cores <= 0 {
					cores = 1
				}
				st.CPUPercent = math.Round(cpuDelta/sysDelta*cores*10000) / 100
			}
		}
		statsCache.prev[r.id] = r.raw
		next[r.id] = st
	}
	// 清掉已消失容器的残留基线，避免容器 ID 被复用时算出离谱的差值。
	for id := range statsCache.prev {
		if _, ok := next[id]; !ok {
			delete(statsCache.prev, id)
		}
	}
	statsCache.cur = next
}

type rawReading struct {
	raw cpuSample
	st  ContainerStats
}

// readContainerStats 读单个容器的一次 stats 快照。
func readContainerStats(ctx context.Context, svcCtx *svc.ServiceContext, id string) (rawReading, error) {
	resp, err := svcCtx.DockerClient.ContainerStatsOneShot(ctx, id)
	if err != nil {
		return rawReading{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var v container.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return rawReading{}, err
	}

	var out rawReading
	out.raw = cpuSample{
		total:     v.CPUStats.CPUUsage.TotalUsage,
		system:    v.CPUStats.SystemUsage,
		onlineCPU: float64(v.CPUStats.OnlineCPUs),
	}
	out.st.MemUsed, out.st.MemLimit, out.st.MemPercent = memoryUsage(v.MemoryStats)
	return out, nil
}

// memoryUsage 从 memory_stats 里算出「已用 / 上限 / 占用率」。
//
// 两个坑：
//   - 必须扣掉 page cache，否则数字虚高（能差出几百 MB，看着像内存泄漏）。
//     cgroup v2 的对应项叫 inactive_file，v1 叫 cache。
//   - 没设内存上限时，limit 报的是宿主机总内存（v1）或一个接近 2^63 的极大值（v2），
//     直接显示会变成 "128MB / 32GB" 这种误导信息，所以统一置 0。
func memoryUsage(m container.MemoryStats) (used, limit uint64, percent float64) {
	used = m.Usage
	if cache, ok := m.Stats["inactive_file"]; ok {
		if cache <= used {
			used -= cache
		}
	} else if cache, ok := m.Stats["cache"]; ok {
		if cache <= used {
			used -= cache
		}
	}

	limit = m.Limit
	if limit > 1<<50 {
		limit = 0
	} else if total := hostMemTotal(); total > 0 && limit >= total*99/100 {
		limit = 0
	}
	if limit > 0 {
		percent = math.Round(float64(used)/float64(limit)*10000) / 100
	}
	return used, limit, percent
}

var (
	hostMemOnce  sync.Once
	hostMemBytes uint64
)

// hostMemTotal 读宿主机 MemTotal（容器未做 lxcfs 隔离时 /proc/meminfo 就是宿主机的），
// 用来识别「limit 其实是宿主机总内存 = 没设上限」这种情况。
func hostMemTotal() uint64 {
	hostMemOnce.Do(func() {
		b, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.HasPrefix(line, "MemTotal:") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return
			}
			kb, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return
			}
			hostMemBytes = kb * 1024
			return
		}
	})
	return hostMemBytes
}
