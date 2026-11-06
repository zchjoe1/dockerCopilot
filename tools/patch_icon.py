#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""把 dockercopilot 镜像图标挂到本地改过的镜像名上。

上游内置图标表里 key 是 `0nlylty/dockercopilot`，而本地构建推的是
`zchjoe/dockercopilot`，匹配函数 ko() 走的是「去 tag 后精确匹配 → 短名匹配 →
子串匹配」三步，三步都对不上，所以卡片一直显示默认的通用方块。
这里补一个同名的 key，指向同一个图标变量 D1。
"""
import sys

BUNDLE = "front/assets/index-BUWPFL6N.js"
ANCHOR = '"0nlylty/dockercopilot":D1,'
REPLACE = '"0nlylty/dockercopilot":D1,"zchjoe/dockercopilot":D1,'


def main() -> int:
    src = open(BUNDLE, encoding="utf-8").read()
    if REPLACE in src:
        print("· 图标映射已经是打过补丁的状态，跳过")
        return 0
    n = src.count(ANCHOR)
    if n != 1:
        raise SystemExit(f"✗ 锚点出现 {n} 次，期望 1 次，拒绝替换")
    src = src.replace(ANCHOR, REPLACE)
    open(BUNDLE, "w", encoding="utf-8").write(src)
    print("✓ 已给 zchjoe/dockercopilot 挂上 DC logo")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
