import type { ComponentPropsWithRef, ReactNode } from 'react'

import s from './ui.module.css'

type Props = ComponentPropsWithRef<'button'> & {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
  size?: 'sm' | 'md'
  icon?: ReactNode
}

export function Button({ variant = 'secondary', size = 'md', icon, className, children, type = 'button', ...rest }: Props) {
  const cls = [s.button, s[variant], size === 'sm' ? s.sm : '', !children ? s.iconOnly : '', className ?? ''].filter(Boolean).join(' ')
  return (
    <button type={type} className={cls} {...rest}>
      {icon}
      {children}
    </button>
  )
}
