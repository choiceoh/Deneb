// Deneb's "응답 중" sparkle — a four-point glint (✦) that bursts bright and swells,
// then settles dim, over a soft warm glow. Ported from the native client's
// StarIndicator (Deneb/client-android · ToolMessage.kt): the same sparkle geometry
// and burst/glow timing, kept in Deneb's own blue-white — the colour of the real star
// (α Cygni is a blue-white supergiant; the native client's StarIndicator is sky-blue too).
// It's the one deliberate cool note in the warm-Zen palette: the star wears Deneb's colour,
// not the clay accent. The burst + glow run on CSS keyframes — so prefers-reduced-motion
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
          {/* Blue-white sheen with a faint prismatic spread — an icy-cyan highlight through
              Deneb's sky-blue to an indigo-leaning deep. The axis is tightened to the glyph
              (6,6)→(18,18) so the whole stop range lands on the star, not off in the corners;
              the SMIL rotation then sweeps the cyan↔indigo poles across it as a slow shimmer. */}
          <linearGradient id="deneb-sheen" x1="6" y1="6" x2="18" y2="18" gradientUnits="userSpaceOnUse">
            <stop offset="0" stopColor="#e4f3ff" />
            <stop offset="0.32" stopColor="#97d3e8" />
            <stop offset="0.62" stopColor="#79b8f0" />
            <stop offset="1" stopColor="#4a72cf" />
            <animateTransform
              attributeName="gradientTransform"
              type="rotate"
              from="0 12 12"
              to="360 12 12"
              dur="11s"
              repeatCount="indefinite"
            />
          </linearGradient>
          {/* Soft sky-blue bloom — kept tight (fades out by ~half the radius, well inside
              the tips at r8) so it sits behind the star's body and the chunky points clearly
              extend past it, instead of meeting its rim like petals on a flower. */}
          <radialGradient id="deneb-glow">
            <stop offset="0" stopColor="#7ec0f5" stopOpacity="0.85" />
            <stop offset="0.28" stopColor="#7ec0f5" stopOpacity="0.32" />
            <stop offset="0.5" stopColor="#7ec0f5" stopOpacity="0" />
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
