#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""在容器卡片上、和「运行: xxx」同一列里加一行资源占用。

对齐位置：插在「运行/状态」元素所在的那个 children 数组（文字列 flex-1 min-w-0）里，
所以它和上面那行左对齐 —— 用户明确要求「资源占用数据要对齐运行时间」。

关于宽度（这一行返工过很多次，实测数据记在这里免得再猜）：
    「文字列」可用宽度随视口变化：
        1280px → 102px      1400px → 132px      1600px 及以上 → 174px
    `CPU 0.6% · 内存 17.7MB` 实测需要 143px：
        ≥1600px 放得下 → 一行 ✓
        ≤1400px 放不下 → 这里【不用 truncate】，改用 flex-wrap 自动折两行，
                        两段各自完整，不会再出现 `内存 17.7…` 被省略号吃掉。
    也试过不带标签的 `0.6% · 17.7MB`（87px，任何宽度都放得下），
    但用户要求能看出「哪个是 CPU、哪个是内存」，所以标签必须留。

front/assets/index-*.js 是上游压缩产物、没有 sourcemap，只能定点插入。
插入前断言锚点在【全文件唯一】。
"""
import sys

BUNDLE = "front/assets/index-BUWPFL6N.js"

# 锚点：原「运行/状态」那一行的结尾。结构是
#     ...children:"状态: 已停止"})})]})]}),!a&&...
#                                ↑ 这个 ] 关掉的是「文字列」flex-1 min-w-0 的 children
ANCHOR = 'children:"状态: 已停止"})})]'

# 资源行。要点：
#   · IIFE 包一层，f() 自带，不依赖压缩后的变量名
#   · cpuPercent / memUsed 为 null（已停止、还没采到）时整行渲染成 null，不占位
#   · 用 flex-wrap + gap 排版：放得下就是一行，放不下折两行，
#     且不在两段之间插「·」——折行时不会在行尾留一个孤零零的分隔符
#   · 没有内存上限时只显示已用；有上限时写 2.6/3.0GB
#   · title 放完整信息（含上限），悬停可看全；占用高变红/变黄
RESOURCE = "".join([
    '(function(){',
    'if(k.cpuPercent==null&&k.memUsed==null)return null;',
    'var f=function(b){if(b==null)return"";',
    'var u=["B","KB","MB","GB","TB"],i=0,n=b;',
    'while(n>=1024&&i<u.length-1){n/=1024;i++}',
    'return(i===0?n:n.toFixed(n>=100?0:1))+u[i]},',
    'fnum=function(b){return f(b).replace(/[A-Za-z]+$/,"")},',
    'cpu=k.cpuPercent!=null?k.cpuPercent.toFixed(1)+"%":null,',
    'mem=k.memUsed!=null?(k.memLimit>0?fnum(k.memUsed)+"/"+f(k.memLimit):f(k.memUsed)):null,',
    'memFull=k.memUsed!=null?(f(k.memUsed)+(k.memLimit>0?" / "+f(k.memLimit):"")):null,',
    'hot=(k.cpuPercent!=null&&k.cpuPercent>=80)||(k.memPercent!=null&&k.memPercent>=90),',
    'warm=(k.cpuPercent!=null&&k.cpuPercent>=50)||(k.memPercent!=null&&k.memPercent>=70),',
    'cls=hot?"text-red-500 dark:text-red-400":',
    'warm?"text-amber-500 dark:text-amber-400":"text-gray-400 dark:text-gray-500";',
    'return o.jsxs("div",{className:"text-xs "+cls,',
    'style:{display:"flex",flexWrap:"wrap",gap:"0 6px",alignItems:"baseline"},',
    'title:(cpu?"CPU "+cpu:"")+(cpu&&memFull?" · ":"")+(memFull?"内存 "+memFull:""),',
    'children:[',
    'cpu?o.jsxs("span",{children:["CPU ",cpu]}):null,',
    'mem?o.jsxs("span",{children:["内存 ",mem]}):null',
    ']})',
    '})()',
])


def check_balanced(expr: str, label: str) -> None:
    """替换进去的是 JS 表达式，括号必须自平衡。

    这个检查是补出来的：第一版漏了一个 `{`（写成 o.jsxs("span",children:[...])
    而不是 o.jsxs("span",{children:[...])），整包 bundle 直接语法错误、页面白屏。
    压缩产物没有 sourcemap，光看 diff 很难发现，所以让工具自己先验一遍。
    """
    close = {')': '(', ']': '[', '}': '{'}
    stack = []
    instr = None
    esc = False
    for pos, ch in enumerate(expr):
        if instr:
            if esc:
                esc = False
            elif ch == '\\':
                esc = True
            elif ch == instr:
                instr = None
            continue
        if ch in '"\'`':
            instr = ch
            continue
        if ch in '([{':
            stack.append((ch, pos))
        elif ch in ')]}':
            if not stack:
                raise SystemExit(f"✗ {label}：位置 {pos} 出现多余的 {ch}")
            op, opos = stack.pop()
            if op != close[ch]:
                raise SystemExit(
                    f"✗ {label}：位置 {pos} 的 {ch} 与位置 {opos} 的 {op} 不匹配\n"
                    f"    上下文: {expr[max(0, pos-70):pos+30]!r}"
                )
    if stack:
        detail = "\n".join(
            f"    位置 {p} 的 {c} 未闭合: {expr[max(0, p-60):p+40]!r}" for c, p in stack
        )
        raise SystemExit(f"✗ {label}：有 {len(stack)} 个括号未闭合\n{detail}")


def main() -> int:
    check_balanced(RESOURCE, "资源行表达式")
    src = open(BUNDLE, encoding="utf-8").read()

    if RESOURCE in src:
        print("· 已经是打过补丁的状态，跳过")
        return 0

    n = src.count(ANCHOR)
    if n == 0:
        raise SystemExit("✗ 锚点未找到 —— bundle 可能已被换过")
    if n > 1:
        raise SystemExit(f"✗ 锚点出现 {n} 次，不唯一，拒绝替换")

    # 插在「运行/状态」元素之后、文字列 children 数组的 ] 之前 → 与它同级、左对齐
    prefix = ANCHOR[:-1]  # 去掉结尾那个 ]
    out = src.replace(ANCHOR, prefix + "," + RESOURCE + "]")
    open(BUNDLE, "w", encoding="utf-8").write(out)
    print(f"✓ 已插入资源行（对齐到「运行」那一列）：{len(src)} → {len(out)} 字节 ({len(out)-len(src):+d})")
    print("  括号自检：通过")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
