export interface Bar {
  label: string;
  value: number;
}

export function BarChart({
  bars,
  width = 480,
  barHeight = 22,
  gap = 6,
  ariaLabel = 'bar chart',
}: {
  bars: Bar[];
  width?: number;
  barHeight?: number;
  gap?: number;
  ariaLabel?: string;
}) {
  if (bars.length === 0) return null;
  const max = Math.max(...bars.map(b => b.value), 1);
  const labelW = 140;
  const valueW = 50;
  const chartW = width - labelW - valueW;
  const height = bars.length * (barHeight + gap);

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      width="100%"
      height={height}
      preserveAspectRatio="xMinYMin meet"
      role="img"
      aria-label={ariaLabel}
    >
      {bars.map((b, i) => {
        const w = (b.value / max) * chartW;
        const y = i * (barHeight + gap);
        return (
          <g key={b.label} transform={`translate(0,${y})`}>
            <text x={0} y={barHeight - 6} className="fill-slate-700" fontSize="12">
              {b.label}
            </text>
            <rect
              x={labelW}
              y={2}
              width={Math.max(w, 1)}
              height={barHeight - 4}
              className="fill-sky-500"
              rx={2}
            />
            <text x={labelW + chartW + 6} y={barHeight - 6} className="fill-slate-600" fontSize="12">
              {b.value}
            </text>
          </g>
        );
      })}
    </svg>
  );
}
