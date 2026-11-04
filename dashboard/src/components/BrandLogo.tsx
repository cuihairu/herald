import { theme } from 'antd'

// 仓库资产 logo.svg 原样引用（构建时由 public/ 复制进 dist），
// 不做任何改绘。红色图形在三套主题底色上均有足够对比。
export default function BrandLogo({ size = 24 }: { size?: number }) {
  const { token } = theme.useToken()
  return (
    <img
      src="/logo.svg"
      alt="Herald logo"
      width={size}
      height={size}
      style={{ display: 'block', borderRadius: token.borderRadiusSM }}
    />
  )
}
