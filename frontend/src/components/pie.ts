// The vote's pie chart (F12, docs/06 "Vote page").
//
// Inline SVG drawn here rather than by a chart library: a pie of at most five
// slices is a few lines of arithmetic, and a library would be most of the
// bundle for one picture. The chart is aria-hidden -- the list beside it
// carries every number it shows and is its text alternative -- so it only has
// to be right to look at.

const SVG = "http://www.w3.org/2000/svg";

/** One slice. */
export interface Slice {
  /** The number of votes. A zero draws nothing. */
  value: number;
  /** Which of the five palette colours, 1 to 5: the restaurant's place in the list. */
  colour: number;
  /** Pulled out from the centre: the restaurant ahead, so the chart does not rely on colour. */
  pulled: boolean;
}

const RADIUS = 0.9;
const PULL = 0.07;

/** A point on the circle at the given fraction of a turn, from twelve o'clock clockwise. */
function point(turn: number, radius: number): [number, number] {
  const angle = 2 * Math.PI * turn - Math.PI / 2;
  return [radius * Math.cos(angle), radius * Math.sin(angle)];
}

function round(n: number): string {
  return (Math.round(n * 10000) / 10000).toString();
}

/** The path of one slice between two fractions of a turn. */
export function slicePath(from: number, to: number, radius = RADIUS): string {
  const [x1, y1] = point(from, radius);
  const [x2, y2] = point(to, radius);
  const large = to - from > 0.5 ? 1 : 0;
  return `M 0 0 L ${round(x1)} ${round(y1)} A ${radius} ${radius} 0 ${large} 1 ${round(x2)} ${round(y2)} Z`;
}

/**
 * Draws the pie.
 *
 * With no votes at all it is an empty ring with the label in it, which says
 * "nothing yet" more plainly than a blank square. A restaurant with every
 * vote is a whole circle: an arc from a point back to itself draws nothing.
 */
export function pieChart(slices: readonly Slice[], emptyLabel: string): SVGSVGElement {
  const svg = document.createElementNS(SVG, "svg");
  svg.setAttribute("viewBox", "-1 -1 2 2");
  svg.setAttribute("class", "vote-pie");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");

  const total = slices.reduce((sum, slice) => sum + Math.max(0, slice.value), 0);

  if (total === 0) {
    const ring = document.createElementNS(SVG, "circle");
    ring.setAttribute("r", String(RADIUS));
    ring.setAttribute("class", "vote-pie-empty");
    svg.appendChild(ring);
    const text = document.createElementNS(SVG, "text");
    text.setAttribute("class", "vote-pie-label");
    text.setAttribute("text-anchor", "middle");
    text.setAttribute("dominant-baseline", "middle");
    text.textContent = emptyLabel;
    svg.appendChild(text);
    return svg;
  }

  let at = 0;
  for (const slice of slices) {
    if (slice.value <= 0) {
      continue;
    }
    const share = slice.value / total;
    let shape: SVGElement;
    if (share >= 1) {
      shape = document.createElementNS(SVG, "circle");
      shape.setAttribute("r", String(RADIUS));
    } else {
      shape = document.createElementNS(SVG, "path");
      shape.setAttribute("d", slicePath(at, at + share));
      if (slice.pulled) {
        const [dx, dy] = point(at + share / 2, PULL);
        shape.setAttribute("transform", `translate(${round(dx)} ${round(dy)})`);
      }
    }
    shape.setAttribute("class", `vote-slice vote-slice-${slice.colour}${slice.pulled ? " vote-slice-ahead" : ""}`);
    svg.appendChild(shape);
    at += share;
  }
  return svg;
}
