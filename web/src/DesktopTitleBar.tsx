import faviconUrl from '/icon-192.png'

interface Props {
  onMinimise: () => void
  onMaximise: () => void
  onClose: () => void
}

export default function DesktopTitleBar({ onMinimise, onMaximise, onClose }: Props) {
  return (
    <div className="desktop-titlebar" style={{ '--wails-draggable': 'drag' } as React.CSSProperties}>
      <div className="desktop-titlebar-brand">
        <img src={faviconUrl} className="dtb-logo-img" alt="" />
        <span className="desktop-titlebar-name">PHAZE</span>
      </div>
      <div className="desktop-titlebar-controls" style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}>
        <button className="dtb-btn dtb-min" onClick={onMinimise} title="Minimise">
          <svg width="12" height="2" viewBox="0 0 12 2"><rect width="12" height="2" rx="1" fill="currentColor"/></svg>
        </button>
        <button className="dtb-btn dtb-max" onClick={onMaximise} title="Maximise">
          <svg width="11" height="11" viewBox="0 0 11 11"><rect x="1" y="1" width="9" height="9" rx="2" fill="none" stroke="currentColor" strokeWidth="1.5"/></svg>
        </button>
        <button className="dtb-btn dtb-close" onClick={onClose} title="Close">
          <svg width="11" height="11" viewBox="0 0 11 11">
            <line x1="1.5" y1="1.5" x2="9.5" y2="9.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"/>
            <line x1="9.5" y1="1.5" x2="1.5" y2="9.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"/>
          </svg>
        </button>
      </div>
    </div>
  )
}
