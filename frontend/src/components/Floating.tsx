import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";

/** A fixed-position layer that closes on outside click or Esc. */
export function Floating(props: {
  x: number;
  y: number;
  onClose: () => void;
  className: string;
  children: ReactNode;
  label?: string;
  /** "above-center": x is the centre and y the top edge of the anchor; the layer opens above it. */
  place?: "above-center";
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ left: props.x, top: props.y });

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const x = props.place === "above-center" ? props.x - r.width / 2 : props.x;
    const y = props.place === "above-center" ? props.y - r.height - 12 : props.y;
    setPos({
      left: Math.max(8, Math.min(x, window.innerWidth - r.width - 8)),
      top: Math.max(8, Math.min(y, window.innerHeight - r.height - 8)),
    });
  }, [props.x, props.y, props.place]);

  useEffect(() => {
    const down = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) props.onClose();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") props.onClose();
    };
    const t = setTimeout(() => document.addEventListener("mousedown", down), 0);
    document.addEventListener("keydown", key);
    return () => {
      clearTimeout(t);
      document.removeEventListener("mousedown", down);
      document.removeEventListener("keydown", key);
    };
  }, [props.onClose]);

  return createPortal(
    <div ref={ref} className={props.className} style={pos} role="dialog" aria-label={props.label}>
      {props.children}
    </div>,
    document.body,
  );
}

export interface MenuItem {
  label: string;
  hint?: string;
  danger?: boolean;
  run: () => void;
}

export function Menu(props: { x: number; y: number; items: (MenuItem | "sep")[]; onClose: () => void }) {
  return (
    <Floating x={props.x} y={props.y} onClose={props.onClose} className="menu" label="Меню">
      {props.items.map((it, i) =>
        it === "sep" ? (
          <hr key={i} />
        ) : (
          <button
            key={i}
            className={it.danger ? "danger" : ""}
            autoFocus={i === 0}
            onClick={() => {
              props.onClose();
              it.run();
            }}
          >
            {it.label}
            {it.hint && <span>{it.hint}</span>}
          </button>
        ),
      )}
    </Floating>
  );
}
