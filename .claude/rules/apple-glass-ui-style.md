# Apple Glass Morphism UI 设计规则

> 适用项目: claude-monitor (Novascope)
> 风格基线: Apple Glass Morphism / 半透明深度感
> 关联文件: agents/design-reviewer.md

---

## 1. Token 体系（强制绑定，不得绕开）

所有 CSS 必须使用 `var(--token)` 引用 `tokens.css` 中定义的变量，严禁散落 hex。

### Colors

| Token | Value | Usage |
|-------|-------|-------|
| `--color-primary` | `#007AFF` | Buttons, links, active tab, focus |
| `--color-primary-hover` | `#0056CC` | Primary button hover |
| `--color-primary-active` | `#004299` | Primary button press |
| `--color-primary-bg` | `rgba(0,122,255,0.08)` | Subtle bg (tab active, hover) |
| `--color-success` | `#34C759` | Success, green status |
| `--color-warning` | `#FF9500` | Warning |
| `--color-danger` | `#FF3B30` | Error, danger button, red status |
| `--color-text-primary` | `#1C1C1E` | Headings, body |
| `--color-text-secondary` | `#6E6E73` | Labels, subtitles |
| `--color-text-tertiary` | `#AEAEB2` | Placeholders, units, muted |
| `--color-text-placeholder` | `#C7C7CC` | Input ::placeholder |
| `--color-border` | `rgba(0,0,0,0.08)` | Borders, table headers |
| `--color-glass-bg` | `rgba(255,255,255,0.72)` | Cards, topbar, modals |
| `--color-glass-border` | `rgba(255,255,255,0.4)` | Glass element borders |
| `--color-background` | `#E8E8ED` | Page body background |

### Blur

| Token | Value | Use |
|-------|-------|-----|
| `--blur-glass` | `blur(20px)` | Cards, topbar |
| `--blur-heavy` | `blur(40px)` | Modal panels |
| `--blur-light` | `blur(10px)` | Modal overlays |

### Spacing

`--space-xs:4px` `--space-sm:8px` `--space-md:16px` `--space-lg:24px` `--space-xl:32px` `--space-2xl:48px`

### Font Sizes

`--font-size-xs:11px` `--font-size-sm:12px` `--font-size-md:14px` `--font-size-lg:16px` `--font-size-xl:20px` `--font-size-2xl:24px`

### Font Weights

`--font-weight-regular:400` `--font-weight-medium:500` `--font-weight-semibold:600` `--font-weight-bold:700`

### Border Radius

`--radius-sm:8px` `--radius-md:12px` `--radius-lg:20px` `--radius-xl:24px` `--radius-full:9999px`

### Shadows

`--shadow-sm` (1px/3px) `--shadow-md` (4px/12px) `--shadow-lg` (8px/32px) `--shadow-xl` (16px/48px)

### Transitions

`--transition-fast:0.15s ease` `--transition-normal:0.25s ease` `--transition-slow:0.35s ease`

### Z-index

`--z-dropdown:100` `--z-sticky:200` `--z-modal-backdrop:300` `--z-modal:400` `--z-tooltip:500`

### Layout

`--topbar-height:64px` `--content-max-width:1200px`

**引用规则**:
- 任何颜色 → `var(--color-*)`；新增颜色前先确认是否可派生。
- 间距、字号、圆角必须走 token 步进，不新造 px 值。
- 不使用 emoji 作为功能图标。

---

## 2. 全局样式规范

- 页面背景：`var(--color-background)`
- 所有卡片/面板使用 GlassCard 组件包裹
- 层级通过 blur + transparency 表达，不靠重边框或深色背景
- 字体：系统默认（San Francisco / PingFang SC），不引入外部字体

---

## 3. 组件 API 参考

### CSS Modules ClassName Pattern

```tsx
import styles from './Component.module.css'

// Multi-class: use array .filter(Boolean).join(' ')
const classNames = [styles.base, styles[variant], condition && styles.mod, className]
  .filter(Boolean).join(' ')

// Simple two-class: template literal OK
;<div className={`${styles.wrapper} ${error ? styles.error : ''}`} />
```

### GlassCard — 核心容器，所有卡片/面板必须使用

```tsx
interface GlassCardProps extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode
  flat?: boolean          // no shadow, hover→shadow-sm
  noPadding?: boolean     // padding:0 (for wrapping Table)
  smallPadding?: boolean  // padding: var(--space-md) instead of --space-lg
}
```

CSS: `bg:--color-glass-bg` `backdrop-filter:--blur-glass` `border:1px solid --color-glass-border` `radius:--radius-lg` `shadow:--shadow-md`（hover→`--shadow-lg`）

### Button

```tsx
type ButtonVariant = 'primary' | 'secondary' | 'text' | 'danger'  // default: primary
type ButtonSize = 'small' | 'medium' | 'large'                    // default: medium
```

- `primary`: bg `--color-primary` white text
- `secondary`: transparent, `--color-primary` border + text
- `text`: transparent, no border
- `danger`: bg `--color-danger` white text
- Base: `radius:--radius-full` `transition:all --transition-fast`
- Disabled: `opacity:0.4; cursor:not-allowed`
- Sizes: small=`4px 12px`/`--font-size-sm`, medium=`8px 20px`/`--font-size-md`, large=`12px 28px`/`--font-size-lg`

### Input

```tsx
interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string; suffix?: string; error?: string
}
```

Bottom-border only: `border:none; border-bottom:1.5px solid --color-border; border-radius:0`
Focus → `--color-primary`
Label: `--font-size-sm` `--font-weight-medium` `--color-text-secondary`

### Select

与 Input 相同的 bottom-border 风格，自定义 SVG chevron，`appearance:none`

```tsx
interface SelectOption { value: string; label: string }
interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string; options: SelectOption[]
}
```

### Table — 必须包裹在 `<GlassCard noPadding>` 中

```tsx
interface Column<T> { key:string; title:string; dataIndex:keyof T|string; render?:(v:unknown, r:T)=>ReactNode; width?:number|string }
interface TableProps<T> { columns:Column<T>[]; dataSource:T[]; rowKey?:string; emptyText?:string }
```

Headers: uppercase, `--font-size-xs`, `--font-weight-semibold`, `--color-text-secondary`
Rows: border `rgba(0,0,0,0.04)`, hover bg `rgba(0,0,0,0.02)`, 最后一行无边框

### ChartCard — 标准 GlassCard + ECharts + ResizeObserver

```tsx
interface ChartCardProps { title?:string; option:EChartsOption; height?:number; className?:string }
```

### MiniChart — flat + smallPadding GlassCard，默认高度 180

```tsx
interface MiniChartProps { title?:string; option:EChartsOption; height?:number }
```

### CenterModal — Escape/overlay 关闭，scaleIn 动画

```tsx
interface CenterModalProps { open:boolean; onClose:()=>void; title?:string; children:ReactNode; footer?:ReactNode }
```

Overlay: `z:--z-modal-backdrop` `bg:rgba(0,0,0,0.2)` `blur:--blur-light`
Panel: `max-w:480px` `radius:--radius-xl` `shadow:--shadow-xl` `blur:--blur-heavy` `padding:--space-xl`

### BottomSheet — overlay 关闭，slideUp 动画

```tsx
interface BottomSheetProps { open:boolean; onClose:()=>void; title?:string; children:ReactNode }
```

Panel: `max-w:600px` slideUp 动画，drag handle: 36x4px `rgba(0,0,0,0.15)`

### ProgressBar

```tsx
interface ProgressBarProps { percent:number; label?:string; showPercent?:boolean; status?:'normal'|'danger'|'success' }
```

Track: `h:4px` `bg:rgba(0,0,0,0.06)` `radius:--radius-full`
Fill: `--color-primary`（或 danger/success）`transition:width 0.6s ease`

### StatusDot

```tsx
type StatusDotColor = 'green' | 'red'
interface StatusDotProps { color:StatusDotColor; pulse?:boolean; className?:string }
```

8x8px circle. Green: `bg:--color-success` glow `0 0 6px rgba(52,199,89,0.5)`. Red: `bg:--color-danger` glow `0 0 6px rgba(255,59,48,0.5)`

---

## 4. 页面布局模式（强制）

每个页面 CSS module 必须包含：

```css
.page { padding:var(--space-xl); max-width:var(--content-max-width); margin:0 auto;
  display:flex; flex-direction:column; gap:var(--space-lg); }
.header { margin-bottom:var(--space-sm); }
.title { font-size:var(--font-size-2xl); font-weight:var(--font-weight-bold);
  letter-spacing:-0.5px; }
.subtitle { font-size:var(--font-size-md); color:var(--color-text-secondary);
  margin-top:var(--space-xs); }
.cardTitle { font-size:var(--font-size-lg); font-weight:var(--font-weight-semibold);
  margin-bottom:var(--space-md); }
.form { display:flex; flex-direction:column; gap:var(--space-md); }
```

Grid patterns:

```css
.grid { display:grid; grid-template-columns:1fr 1fr; gap:var(--space-lg); }
.grid2 { display:grid; grid-template-columns:360px 1fr; gap:var(--space-lg); }
```

---

## 5. ECharts 图表规范

所有图表从 `../../lib/echarts` 导入（tree-shake）。通用 option 模式：

```ts
grid: { top:20, right:20, bottom:30, left:50 }
xAxis: { nameTextStyle:{fontSize:10, color:'#AEAEB2'}, axisLine:{show:false},
  axisTick:{show:false}, splitLine:{lineStyle:{color:'rgba(0,0,0,0.04)'}} }
tooltip: { trigger:'axis', backgroundColor:'rgba(255,255,255,0.9)',
  borderColor:'rgba(0,0,0,0.08)', textStyle:{fontSize:11, color:'#1C1C1E'} }
```

Series colors（ECharts 不支持 CSS vars，允许硬编码）:
- Thickness: `#007AFF` | PL: `#AF52DE` | Roughness: `#34C759` | Temperature: `#FF9500` | Humidity: `#007AFF`

---

## 6. App Shell（未来 Wails/Electron 前端参考）

```
HashRouter → AppLayout(TopBar + Outlet)
  Routes: / (dashboard) /sessions /stats /settings
```

TopBar: fixed, `z:--z-sticky`, glass bg. Tabs 使用 `NavLink`，`isActive` → `--color-primary` + `--color-primary-bg`.

---

## 7. 开发红线（绝对禁止）

1. CSS/inline style 中硬编码 hex 颜色 — 必须用 `var(--color-xxx)`
2. Tailwind CSS class names
3. 重边框或深色背景块（破坏 glass morphism）
4. 引入新的 UI 依赖（component library、icon pack）
5. Emoji 在 UI 文本、代码或注释中
6. 组件/页面的 CSS 文件不是 CSS Module
7. 非 GlassCard 包裹的卡片/面板
8. 表单控件不匹配 bottom-border-only 风格
9. 交互元素缺少 hover/active/disabled 状态
10. 间距/圆角/字体使用任意 px 值 — 必须走 token scale

---

## 8. P0 自检门（交付前必过）

1. **Token 完整性**：颜色、间距、字号、圆角 100% 走 `var(--*)`，grep 全文件无散落 hex（ECharts series 除外）
2. **组件规范**：所有卡片走 GlassCard，Table 包裹在 GlassCard noPadding 中
3. **交互状态**：每个交互元素有 default / hover / active / disabled 四态
4. **Glass 层级**：层级靠 blur + transparency 表达，不靠阴影堆叠
5. **CSS Modules**：每个 .tsx 有对应的 .module.css
6. **无新增依赖**：未引入任何 UI 库或图标库
