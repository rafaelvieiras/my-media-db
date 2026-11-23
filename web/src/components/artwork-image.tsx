import { useState } from 'react'
import { ImageOff } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'

// ArtworkImage renders ONE artwork of the gallery/lightbox with a placeholder of its own:
// a missing file (a broken asset, an inventory URL that died in the proxy) swaps the
// `<img>` for the "Image unavailable" box IN THE SAME FRAME (the caller's aspect/zoom
// classes are reused, so the row does not jump).
//
// Why it exists: without `onError` the browser drew the broken-image icon AND the alt text
// ("Poster 2", "Fanart 1") over the cover — a loose number that said nothing about what
// the artwork was.
export function ArtworkImage({
  src,
  alt,
  className,
}: {
  src: string
  alt: string
  // Classes of the frame: the aspect/size of the kind in the gallery, the zoom classes in
  // the lightbox. Applied to the `<img>` and to the placeholder alike.
  className: string
}) {
  const t = useT()
  // The URL that failed (not a boolean): navigating in the lightbox to another artwork
  // starts over, and coming back to the broken one shows the placeholder again.
  const [quebrado, setQuebrado] = useState<string | null>(null)

  if (quebrado === src) {
    return (
      <div
        role="img"
        aria-label={alt}
        className={cn(
          className,
          'flex flex-col items-center justify-center gap-1 bg-muted text-muted-foreground',
        )}
      >
        <ImageOff className="size-6" aria-hidden />
        <span className="px-2 text-center text-xs">
          {t('media.gallery.imageError')}
        </span>
      </div>
    )
  }
  return (
    <img
      src={src}
      alt={alt}
      loading="lazy"
      onError={() => setQuebrado(src)}
      className={className}
    />
  )
}
