/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/**
 * Split an array into fixed-size chunks (last chunk may be smaller).
 */
export function chunk<T>(items: readonly T[], size: number): T[][] {
  if (size <= 0) throw new Error('chunk size must be > 0')
  const out: T[][] = []
  for (let i = 0; i < items.length; i += size) {
    out.push(items.slice(i, i + size))
  }
  return out
}

/**
 * Run `fn` over `items` with at most `limit` invocations in flight at once,
 * preserving result order. Useful to bound parallel network requests so we
 * neither hold one long request open (proxy/browser timeout) nor fire all at
 * once (browser connection cap / upstream rate limits).
 *
 * `fn` receives the item and its original index. Rejections propagate.
 */
export async function mapWithConcurrency<T, R>(
  items: readonly T[],
  limit: number,
  fn: (item: T, index: number) => Promise<R>
): Promise<R[]> {
  if (limit <= 0) throw new Error('concurrency limit must be > 0')
  const results = new Array<R>(items.length)
  let cursor = 0

  const worker = async (): Promise<void> => {
    while (cursor < items.length) {
      const index = cursor++
      results[index] = await fn(items[index], index)
    }
  }

  const workers = Array.from({ length: Math.min(limit, items.length) }, () =>
    worker()
  )
  await Promise.all(workers)
  return results
}
