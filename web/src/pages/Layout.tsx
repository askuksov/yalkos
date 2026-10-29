import type { ReactNode } from 'react'

export function Layout({ title, children }: { title: string; children: ReactNode }) {
  return (
    <main className="page">
      <header>
        <span className="brand">Yalkos</span>
      </header>
      <h1>{title}</h1>
      {children}
    </main>
  )
}
