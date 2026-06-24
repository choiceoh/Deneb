// Deneb's "응답 중" sparkle — a four-point glint (✦) that bursts bright and swells,
// then settles dim, over a soft warm glow. Ported from the native client's
// StarIndicator (Deneb/client-android · ToolMessage.kt): the same sparkle geometry
// and burst/glow timing, recoloured from Andromeda's clay accent so it stays inside
// the warm-Zen single-accent family (the native star is sky-blue, Deneb being a
// blue star). The burst + glow run on CSS keyframes — so prefers-reduced-motion
// calms them — while a slow SMIL rotation of the fill gradient sends a faceted
// "sheen" flowing across the rays.
//
// The geometry mirrors denebSparklePath: four tips (N/E/S/W) at `outer` from the
// centre, joined by quadratic curves whose controls sit close to the centre
// (d = outer·0.12) so the rays read thin and concave.
export function DenebStar({ size = 20 }: { size?: number }) {
  return (
    <span className="deneb-star" aria-hidden="true">
      <svg viewBox="0 0 24 24" width={size} height={size} fill="none">
        <defs>
          {/* Warm clay gradient fanned into its lighter/deeper neighbours; the
              SMIL rotation sweeps the axis so the sheen flows across the glyph. */}
          <linearGradient id="deneb-sheen" x1="0" y1="0" x2="24" y2="24" gradientUnits="userSpaceOnUse">
            <stop offset="0" stopColor="#d99a6e" />
            <stop offset="0.5" stopColor="#c17a5b" />
            <stop offset="1" stopColor="#a85f43" />
            <animateTransform
              attributeName="gradientTransform"
              type="rotate"
              from="0 12 12"
              to="360 12 12"
              dur="6s"
              repeatCount="indefinite"
            />
          </linearGradient>
          {/* Soft warm bloom behind the star — swells + brightens with each burst. */}
          <radialGradient id="deneb-glow">
            <stop offset="0" stopColor="#dca074" stopOpacity="0.85" />
            <stop offset="0.45" stopColor="#dca074" stopOpacity="0.34" />
            <stop offset="1" stopColor="#dca074" stopOpacity="0" />
          </radialGradient>
        </defs>
        <circle className="deneb-star-glow" cx="12" cy="12" r="11" fill="url(#deneb-glow)" />
        <path
          className="deneb-star-core"
          d="M12 4 Q12.96 11.04 20 12 Q12.96 12.96 12 20 Q11.04 12.96 4 12 Q11.04 11.04 12 4 Z"
          fill="url(#deneb-sheen)"
          stroke="url(#deneb-sheen)"
          strokeWidth="1.3"
          strokeLinejoin="round"
          strokeLinecap="round"
        />
      </svg>
    </span>
  );
}
