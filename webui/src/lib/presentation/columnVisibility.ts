export function getNextVisibleColumnIDs(hideableColumnIDs: readonly string[], requestedVisibleColumnIDs: readonly string[]) {
  if (requestedVisibleColumnIDs.length > 0 || hideableColumnIDs.length === 0) return requestedVisibleColumnIDs
  return [hideableColumnIDs[0]]
}
