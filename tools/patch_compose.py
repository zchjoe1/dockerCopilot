#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""给压缩后的前端 bundle 加「Compose」导航项和路由。

front/assets/index-*.js 是上游压缩产物、没有 sourcemap，只能定点替换。
两处锚点在替换前都会断言出现次数，对不上就拒绝改动。

真正的页面内容不在 bundle 里：这里只加一个挂载点，
页面用原生 DOM 画在 front/index.html 的 __dcMountCompose 里（和设置页同一套做法）。
"""
import sys

BUNDLE = "front/assets/index-BUWPFL6N.js"

# 锚点 1：导航项数组。侧边栏和手机底栏各有一份，内容完全一样 → 应该出现 2 次。
# 图标沿用「备份」那个（mh）：bundle 里没有合适的 file/code 类图标可复用，
# 自己内联 SVG 又要处理 className 透传，风险不值当。
ANCHOR_NAV = '{id:"#backups",label:"备份",icon:mh},{id:"#about",label:"设置",icon:gh}'
REPLACE_NAV = '{id:"#backups",label:"备份",icon:mh},{id:"#compose",label:"Compose",icon:mh},{id:"#about",label:"设置",icon:gh}'
NAV_EXPECTED = 2

# 锚点 2：路由 switch。加一个 case，挂载点交给 window.__dcMountCompose。
ANCHOR_ROUTE = 'case"#about":return o.jsx(mw,{})'
REPLACE_ROUTE = (
    'case"#about":return o.jsx(mw,{});'
    'case"#compose":return o.jsx("div",{className:"max-w-7xl mx-auto",'
    'ref:function(el){if(el&&window.__dcMountCompose)window.__dcMountCompose(el)}})'
)
ROUTE_EXPECTED = 1


def patch(src: str, anchor: str, repl: str, expected: int, label: str) -> str:
    n = src.count(anchor)
    if n == 0 and src.count(repl) >= 1:
        print(f"· {label}：已经是打过补丁的状态，跳过")
        return src
    if n != expected:
        raise SystemExit(f"✗ {label}：锚点出现 {n} 次，期望 {expected} 次，拒绝替换")
    return src.replace(anchor, repl)


def main() -> int:
    src = open(BUNDLE, encoding="utf-8").read()
    before = len(src)
    src = patch(src, ANCHOR_NAV, REPLACE_NAV, NAV_EXPECTED, "导航项")
    src = patch(src, ANCHOR_ROUTE, REPLACE_ROUTE, ROUTE_EXPECTED, "路由")
    if len(src) != before:
        open(BUNDLE, "w", encoding="utf-8").write(src)
        print(f"✓ 已加 Compose 入口：{before} → {len(src)} 字节 ({len(src)-before:+d})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
