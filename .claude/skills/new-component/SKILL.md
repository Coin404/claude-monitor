---
name: new-component
description: 脚手架技能 — /new-component ComponentName 快速生成组件骨架（.tsx + .module.css）
---

# /new-component — 组件脚手架

快速生成符合 Apple Glass Morphism 规范的 React 组件骨架。

## 用法

```
/new-component ComponentName
```

## 执行流程

1. 确认组件名称（PascalCase）。
2. 确定目标目录：
   - 基础 UI 组件 → `src/renderer/src/components/ui/`
   - 布局组件 → `src/renderer/src/components/layout/`
   - 页面 → `src/renderer/src/pages/`
3. 生成 `.tsx` 文件：named export、TypeScript interface、基本结构。
4. 生成 `.module.css` 文件：使用 `var(--token)`、遵循 Apple Glass Morphism 规范。
5. 汇报生成结果。

## 生成的 .tsx 模板

```tsx
import { type HTMLAttributes } from 'react'
import styles from './ComponentName.module.css'

interface ComponentNameProps extends HTMLAttributes<HTMLDivElement> {
  // TODO: add props
}

export function ComponentName({ className, ...rest }: ComponentNameProps) {
  const classNames = [styles.base, className].filter(Boolean).join(' ')

  return (
    <div className={classNames} {...rest}>
      {/* TODO: implement */}
    </div>
  )
}
```

## 生成的 .module.css 模板

```css
.base {
  /* TODO: add styles using var(--token) */
}
```

## 注意事项

- 如果组件是卡片类，继承 GlassCard 模式（bg: var(--color-glass-bg), backdrop-filter: var(--blur-glass), etc.）
- 如果是页面，遵循页面布局模式（.page, .header, .title, .subtitle）
- 所有颜色使用 `var(--color-*)`
- 禁止 Tailwind class names
- 禁止引入外部依赖
