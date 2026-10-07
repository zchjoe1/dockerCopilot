#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""在容器卡片上加一行资源占用：`CPU 1.9% · 内存 43.6MB`。

为什么插在卡片这一层、而不是塞进原来的「运行: xxx」那一行：
    卡片文字列（图标右边那条）只有 132px，1280px 视口下只剩 102px。
    实测一行带标签的写法要 143px（`CPU 1.9% · 内存 43.6MB`），
    塞进文字列必然被 truncate 截断（内存那段会被省略号吃掉）。
    卡片本身是 block 布局（class 里没有 flex），所以把这一行作为
    「头部行」的兄弟节点插入，就能拿到整张卡片的宽度（1280px 下约 178px），
    143px 的文案放得下，而且不用改动原有的「运行: xxx」那一行。

只做一处定点插入，插入前断言锚点在【全文件唯一】。
"""
import sys

BUNDLE = "front/assets/index-BUWPFL6N.js"

# 锚点：原「运行/状态」那一行的结尾。结构是
#     ...children:"状态: 已停止"})})]})]}),!a&&...
#                                ↑ 这个 ] 关掉的是「文字列」flex-1 min-w-0 的 children
# 把资源行插在它前面，资源行就成了「运行: xxx」的同级兄弟 ——
# 也就是和上面那行左对齐（之前插在卡片层，会挂在最左边、对不齐，被用户指出来了）。
ANCHOR = '!a&&o.jsx("div",{className:"flex gap-1 mt-3 pt-3 border-t'

# 资源行本身。要点：
#   · 用 IIFE 包一层，f() 自带，不依赖压缩后的变量名
#   · cpuPercent / memUsed 为 null（已停止、还没采到）时整行渲染成 null，不占位
#   · title 里放完整信息（含内存上限），悬停可看全；占用高变红/变黄
#
# ⚠️ 关于宽度（返工两次才摸清，数据记在这里免得再踩）：
#   「文字列」——也就是对齐后这一行的可用宽度——随视口变化：
#       1280px → 102px     1400px → 132px     1600px → 约 154px
#   12px 字号下 `CPU 0.1% · 42.6MB` 实测要 154px，1280/1400 视口必然被省略号截断。
#   想用 `text-[11px]` 缩字号是【无效】的：Tailwind 只生成构建时源码里出现过的类，
#   那个类原来的 bundle 里没有，压缩后的 CSS 里根本没有这条规则，字号仍是 12px
#   （实测注入后仍是 154px 就是证据）。
#   也试过把「CPU / 内存」两个词换成 11px 内联 SVG 图标：
#   带图标要 117px，1280px 视口只有 102px，照样截断（22/23 张卡中招），所以放弃。
#   最终就是最朴素的 `0.9% · 18.1MB`：实测 1280/1400/1600 三种视口下 0 溢出，
#   顺序固定为「CPU% · 内存」，完整说明（含内存上限）放在 title 里悬停可见。
#   有内存上限时只显示占用率 `87%`（绝对值同样在 title 里），否则也会超宽。
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
    'return o.jsxs("div",{className:"text-xs mt-2 truncate "+cls,',
    'title:(cpu?"CPU "+cpu:"")+(cpu&&memFull?" · ":"")+(memFull?"内存 "+memFull:""),',
    'children:[',
    'cpu?o.jsxs("span",{children:["CPU ",cpu]}):null,',
    'cpu&&mem?" · ":null,',
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

    # 插在「运行/状态」元素之后、文字列 children 数组的 ] 之前，
    # 这样资源行是它的同级兄弟 → 左对齐。
    out = src.replace(ANCHOR, RESOURCE + "," + ANCHOR)
    open(BUNDLE, "w", encoding="utf-8").write(out)
    print(f"✓ 已插入资源行（整卡宽度，带标签）：{len(src)} → {len(out)} 字节 ({len(out)-len(src):+d})")
    print("  括号自检：通过")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
