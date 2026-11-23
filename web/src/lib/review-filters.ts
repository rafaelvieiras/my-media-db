// Pure helpers of the review queue: the filters of the header (title search,
// group kind and reason) and the grouping/ordering of the sections.
//
// They live outside the route so the rules are testable without rendering the
// screen. Nothing here talks to the API: the queue arrives whole (the backend
// sends one page of items) and the filtering happens on the client, which is
// what keeps the search instant with ~1.7k items.
import type { components } from '@/lib/api-types'
import { ehDraft } from '@/lib/entity-badges'

export type ReviewCard = components['schemas']['EntityCard']

// Why the item is in the queue: a draft (never identified) or a match below the
// confidence threshold. The same classification the row shows and the filter offers.
export type ReviewReason = 'not_identified' | 'low_confidence'

export type ReviewReasonFilter = 'all' | ReviewReason

export interface ReviewFilters {
  /** Title (or group label/path) typed in the search box, already debounced. */
  query: string
  /** `review_group.kind`, or `all`. */
  kind: string
  reason: ReviewReasonFilter
}

export interface ReviewGroup {
  key: string
  kind: string
  label: string
  /** Folder behind the group key — the second line that tells equal labels apart. */
  path: string
  items: ReviewCard[]
}

export const REVIEW_PAGE_SIZE = 50

const NO_FILE_KIND = 'sem_arquivo'
const NO_FILE_KEY = 'sem_arquivo'

// Case- and accent-insensitive: "Série"/"serie" and "Amélie"/"amelie" are the
// same search. The catalog has no collation, so the normalization is ours.
export function normalizeSearch(text: string): string {
  return text
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .trim()
}

// Group kind of a card; an item without a group (older backend) counts as "no file",
// the same fallback the screen uses to never hide an item of the queue.
export function groupKind(card: ReviewCard): string {
  return card.review_group?.kind ?? NO_FILE_KIND
}

// Reason the card is in the queue, from the same signal the row shows.
export function reviewReason(card: ReviewCard): ReviewReason {
  return ehDraft(card.status) ? 'not_identified' : 'low_confidence'
}

// Path behind a group key (`pasta:/Filmes/X` → `/Filmes/X`); '' when the key has none.
export function groupPath(key: string): string {
  const separator = key.indexOf(':')
  return separator === -1 ? '' : key.slice(separator + 1)
}

// Label and folder of the group, normalized for the search: typing the name of a
// series finds its episodes even when the individual titles do not carry it.
function groupSearchText(card: ReviewCard, noFileLabel: string): string {
  const group = card.review_group
  const label = group?.label ?? noFileLabel
  return normalizeSearch(`${label} ${groupPath(group?.key ?? '')}`)
}

export function filterReviewItems(
  cards: ReviewCard[],
  filters: ReviewFilters,
  noFileLabel: string,
): ReviewCard[] {
  const needle = normalizeSearch(filters.query)
  return cards.filter((card) => {
    if (filters.kind !== 'all' && groupKind(card) !== filters.kind) {
      return false
    }
    if (filters.reason !== 'all' && reviewReason(card) !== filters.reason) {
      return false
    }
    if (needle === '') {
      return true
    }
    return (
      normalizeSearch(card.title).includes(needle) ||
      groupSearchText(card, noFileLabel).includes(needle)
    )
  })
}

export function groupReviewItems(
  cards: ReviewCard[],
  noFileLabel: string,
): ReviewGroup[] {
  const byKey = new Map<string, ReviewGroup>()
  for (const card of cards) {
    const group = card.review_group
    const key = group?.key ?? NO_FILE_KEY
    const existing = byKey.get(key)
    if (existing) {
      existing.items.push(card)
      continue
    }
    byKey.set(key, {
      key,
      kind: group?.kind ?? NO_FILE_KIND,
      label: group?.label ?? noFileLabel,
      path: groupPath(key),
      items: [card],
    })
  }
  return [...byKey.values()]
}

// Largest group first: the group that weighs most in the queue is the one the
// owner opens, so it cannot be buried behind the kind order ("no file" last).
export function sortReviewGroups(
  groups: ReviewGroup[],
  locale: string,
): ReviewGroup[] {
  return [...groups].sort((a, b) => {
    if (a.items.length !== b.items.length) {
      return b.items.length - a.items.length
    }
    const byLabel = a.label.localeCompare(b.label, locale)
    return byLabel !== 0 ? byLabel : a.path.localeCompare(b.path, locale)
  })
}

// Second line of the section: the folder, when it adds context to a label that
// repeats across the queue ("Season 01" under six different series). Empty when
// the path is the label itself.
export function groupContext(path: string, label: string): string {
  const clean = path.replace(/^\/+|\/+$/g, '')
  return normalizeSearch(clean) === normalizeSearch(label) ? '' : clean
}
