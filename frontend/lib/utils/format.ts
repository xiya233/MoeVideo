export function formatCount(value: number): string {
  if (!Number.isFinite(value)) {
    return "0";
  }

  const abs = Math.abs(value);
  const sign = value < 0 ? "-" : "";
  const withUnit = (unit: number, suffix: string): string => {
    const compact = (abs / unit).toFixed(1);
    return `${sign}${compact.endsWith(".0") ? compact.slice(0, -2) : compact}${suffix}`;
  };

  if (abs >= 100000000) {
    return withUnit(100000000, "亿");
  }
  if (abs >= 10000) {
    return withUnit(10000, "w");
  }
  if (abs >= 1000) {
    return withUnit(1000, "k");
  }

  return `${Math.trunc(value)}`;
}

export function formatDate(iso: string): string {
  if (!iso) {
    return "";
  }
  const parsed = new Date(iso);
  if (Number.isNaN(parsed.getTime())) {
    return iso;
  }
  return parsed.toLocaleDateString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  });
}

export function formatDateMinute(iso: string): string {
  if (!iso) {
    return "";
  }
  const parsed = new Date(iso);
  if (Number.isNaN(parsed.getTime())) {
    return iso;
  }
  return parsed.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}
