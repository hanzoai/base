// How the admin prints a quantity, in one place so two pages showing the same
// number show it the same way.

// Powers of 1024, because that is what a filesystem reports. One fraction digit
// below ten of a unit and none above it, so a size stays about four characters
// wide whether it is a fresh file or a working store.
export function bytes(n: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = n;
  let unit = 0;

  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }

  return `${value < 10 && unit > 0 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`;
}

// Grouped, because a record count is read for its magnitude before its digits.
export function count(n: number): string {
  return n.toLocaleString();
}
