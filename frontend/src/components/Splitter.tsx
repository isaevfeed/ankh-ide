import { setState } from "../lib/store";

const LIMITS = {
  files: { min: 160, max: 520, def: 240 },
  changes: { min: 300, max: 1400, def: 440 },
};
const CENTER_MIN = 360;

function save(w: { files: number; changes: number }) {
  try {
    localStorage.setItem("ankh.widths", JSON.stringify(w));
  } catch {
    /* ignore */
  }
}

/** A drag handle on the inner edge of a side panel. Double-click resets the width. */
export function Splitter(props: { pane: "files" | "changes"; other: number }) {
  // other: current width of the opposite side panel (0 when hidden)
  const lim = LIMITS[props.pane];

  const onDown = (e: React.MouseEvent) => {
    e.preventDefault();
    document.body.classList.add("resizing");
    const move = (ev: MouseEvent) => {
      const raw = props.pane === "files" ? ev.clientX : window.innerWidth - ev.clientX;
      const max = Math.min(lim.max, window.innerWidth - props.other - CENTER_MIN);
      const w = Math.round(Math.max(lim.min, Math.min(max, raw)));
      setState((s) => ({ widths: { ...s.widths, [props.pane]: w } }));
    };
    const up = () => {
      document.body.classList.remove("resizing");
      window.removeEventListener("mousemove", move);
      window.removeEventListener("mouseup", up);
      setState((s) => {
        save(s.widths);
        return {};
      });
    };
    window.addEventListener("mousemove", move);
    window.addEventListener("mouseup", up);
  };

  return (
    <div
      className={"split " + (props.pane === "files" ? "split-right" : "split-left")}
      role="separator"
      aria-orientation="vertical"
      title="Потяните, чтобы изменить ширину · двойной клик — по умолчанию"
      onMouseDown={onDown}
      onDoubleClick={() =>
        setState((s) => {
          const widths = { ...s.widths, [props.pane]: lim.def };
          save(widths);
          return { widths };
        })
      }
    />
  );
}
