import { hueOf, initials } from './avatarColor'
import s from './ui.module.css'

export function Avatar({ id, name, size = 20 }: { id: string; name: string; size?: number }) {
  return (
    <span
      className={s.avatar}
      role="img"
      aria-label={name}
      title={name}
      style={{ width: size, height: size, fontSize: size * 0.45, background: `hsl(${hueOf(id)} 45% 45%)` }}
    >
      {initials(name)}
    </span>
  )
}
