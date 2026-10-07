package utiles

import (
	"testing"

	"github.com/docker/docker/api/types/container"
)

// TestMemoryUsage 覆盖三个真实踩过的坑：
//   - cgroup v2 的 page cache 叫 inactive_file，v1 叫 cache，都必须扣掉
//   - 未设上限时 limit 报宿主机总内存（v1）或极大值（v2），必须置 0
//   - 扣 cache 时不能把 used 扣成负数（cache 偶尔会大于 usage）
func TestMemoryUsage(t *testing.T) {
	hostTotal := hostMemTotal()
	if hostTotal == 0 {
		t.Skip("读不到 /proc/meminfo，跳过（非 Linux 环境）")
	}

	const mib = 1024 * 1024

	cases := []struct {
		name        string
		mem         container.MemoryStats
		wantUsed    uint64
		wantLimit   uint64
		wantPercent float64
	}{
		{
			name: "cgroup v2：扣 inactive_file，有上限",
			mem: container.MemoryStats{
				Usage: 300 * mib,
				Limit: 1000 * mib,
				Stats: map[string]uint64{"inactive_file": 100 * mib},
			},
			wantUsed:    200 * mib,
			wantLimit:   1000 * mib,
			wantPercent: 20,
		},
		{
			name: "cgroup v1：扣 cache，有上限",
			mem: container.MemoryStats{
				Usage: 300 * mib,
				Limit: 1000 * mib,
				Stats: map[string]uint64{"cache": 50 * mib},
			},
			wantUsed:    250 * mib,
			wantLimit:   1000 * mib,
			wantPercent: 25,
		},
		{
			name: "两种 key 都在时优先 inactive_file（v2 语义）",
			mem: container.MemoryStats{
				Usage: 300 * mib,
				Limit: 1000 * mib,
				Stats: map[string]uint64{"inactive_file": 100 * mib, "cache": 10 * mib},
			},
			wantUsed:    200 * mib,
			wantLimit:   1000 * mib,
			wantPercent: 20,
		},
		{
			name: "未设上限：limit == 宿主机总内存 → 置 0，不算百分比",
			mem: container.MemoryStats{
				Usage: 300 * mib,
				Limit: hostTotal,
				Stats: map[string]uint64{"inactive_file": 100 * mib},
			},
			wantUsed:    200 * mib,
			wantLimit:   0,
			wantPercent: 0,
		},
		{
			name: "未设上限：cgroup v2 报极大值 → 置 0",
			mem: container.MemoryStats{
				Usage: 300 * mib,
				Limit: 1 << 60,
				Stats: map[string]uint64{"inactive_file": 100 * mib},
			},
			wantUsed:    200 * mib,
			wantLimit:   0,
			wantPercent: 0,
		},
		{
			name: "cache 大于 usage 时不能扣成负数",
			mem: container.MemoryStats{
				Usage: 10 * mib,
				Limit: 1000 * mib,
				Stats: map[string]uint64{"inactive_file": 99 * mib},
			},
			wantUsed:    10 * mib,
			wantLimit:   1000 * mib,
			wantPercent: 1,
		},
		{
			name: "没有 memory.stat（刚启动）时直接用 usage",
			mem: container.MemoryStats{
				Usage: 40 * mib,
				Limit: 1000 * mib,
			},
			wantUsed:    40 * mib,
			wantLimit:   1000 * mib,
			wantPercent: 4,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			used, limit, percent := memoryUsage(tc.mem)
			if used != tc.wantUsed {
				t.Errorf("used = %d, want %d", used, tc.wantUsed)
			}
			if limit != tc.wantLimit {
				t.Errorf("limit = %d, want %d", limit, tc.wantLimit)
			}
			if percent != tc.wantPercent {
				t.Errorf("percent = %v, want %v", percent, tc.wantPercent)
			}
		})
	}
}

// TestGetContainerStatsEmpty 确认没采到数据时返回 false，
// 而不是返回一个 0 值让前端显示「CPU 0.0%」——「停止」和「真的是 0%」必须能区分。
func TestGetContainerStatsEmpty(t *testing.T) {
	if _, ok := GetContainerStats("不存在的容器ID"); ok {
		t.Fatal("未知容器应返回 ok=false")
	}
}
