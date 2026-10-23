import { useState } from "react";
import { Floating } from "./Floating";
import { deliver, fileRef } from "../lib/actions";
import { useUI } from "../lib/store";

export interface EditTarget {
  sessionId: string;
  path: string;
  from?: number;
  to?: number;
  x: number;
  y: number;
  place?: "above-center";
}

/** "Edit…" on a file or selected lines: a prompt that goes straight to the agent. */
export function EditPopover(props: { target: EditTarget; onClose: () => void }) {
  const { sessionId, path, from, to } = props.target;
  const [text, setText] = useState("");
  const autoSend = useUI((s) => s.st?.data.settings.autoSend ?? true);
  const ref = fileRef(sessionId, path, from, to);

  const go = () => {
    const t = text.trim();
    if (!t) return;
    props.onClose();
    void deliver(sessionId, `${ref} — ${t}`);
  };

  return (
    <Floating x={props.target.x} y={props.target.y} place={props.target.place} onClose={props.onClose} className="pop" label="Edit">
      <h3 className="mono">Edit · {ref.replace(/^@/, "")}</h3>
      <textarea
        className="field"
        autoFocus
        value={text}
        placeholder="Что изменить? Например: вынеси валидацию в отдельную функцию"
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            go();
          }
        }}
      />
      <div className="hintline mono">→ {ref} — …</div>
      <div className="acts">
        <span className="hintline">Enter — {autoSend ? "отправить агенту" : "вставить в поле ввода"} · Esc — закрыть</span>
        <button className="btn sm primary" onClick={go} disabled={!text.trim()}>
          {autoSend ? "Отправить" : "Вставить"}
        </button>
      </div>
    </Floating>
  );
}
