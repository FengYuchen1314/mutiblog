// 图片灯箱 island：点击正文图片放大（visible 策略）。
import { useEffect, useState } from 'react'

export default function Lightbox() {
  const [src, setSrc] = useState('')
  useEffect(() => {
    const images = document.querySelectorAll<HTMLImageElement>('.prose img')
    const onClick = (event: Event) => {
      const img = event.target as HTMLImageElement
      if (img.src) setSrc(img.src)
    }
    images.forEach((img) => img.addEventListener('click', onClick))
    return () => {
      images.forEach((img) => img.removeEventListener('click', onClick))
    }
  }, [])
  if (!src) return null
  return (
    <div className="lightbox" onClick={() => setSrc('')} role="presentation">
      <img src={src} alt="" />
    </div>
  )
}
