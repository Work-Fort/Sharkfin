/** Extract initials from a username like "alice-chen" → "AC" or "bob" → "BO". */
export function initials(username: string): string {
  const parts = username.split(/[-_.\s]+/);
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
  return username.slice(0, 2).toUpperCase();
}
