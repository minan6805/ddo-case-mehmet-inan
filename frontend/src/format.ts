export function formatDate(value: string, timeZone?: string): string {
  return new Intl.DateTimeFormat('tr-TR', {
    dateStyle: 'short',
    timeStyle: 'short',
    timeZone,
  }).format(new Date(value));
}
