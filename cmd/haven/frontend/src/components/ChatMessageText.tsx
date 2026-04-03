import type { ReactNode } from "react";

function renderInline(text: string): ReactNode {
  const parts: ReactNode[] = [];
  let index = 0;
  let key = 0;

  while (index < text.length) {
    if (text[index] === "`") {
      const end = text.indexOf("`", index + 1);
      if (end !== -1) {
        parts.push(<code key={key++} className="md-inline-code">{text.slice(index + 1, end)}</code>);
        index = end + 1;
        continue;
      }
    }

    if (text.slice(index, index + 2) === "**") {
      const end = text.indexOf("**", index + 2);
      if (end !== -1) {
        parts.push(<strong key={key++}>{text.slice(index + 2, end)}</strong>);
        index = end + 2;
        continue;
      }
    }

    if (text[index] === "*" && text[index + 1] !== "*") {
      let end = index + 1;
      while (end < text.length && !(text[end] === "*" && text[end + 1] !== "*")) end++;
      if (end < text.length && end > index + 1) {
        parts.push(<em key={key++}>{text.slice(index + 1, end)}</em>);
        index = end + 1;
        continue;
      }
    }

    let textEnd = index + 1;
    while (textEnd < text.length && text[textEnd] !== "`" && text[textEnd] !== "*") textEnd++;
    parts.push(text.slice(index, textEnd));
    index = textEnd;
  }

  return parts.length === 1 ? parts[0] : <>{parts}</>;
}

export default function ChatMessageText({ text }: { text: string }) {
  if (!text) return null;

  const segments: Array<{ type: "text" | "code"; content: string; lang?: string }> = [];
  const codeBlockPattern = /```(\w*)\n?([\s\S]*?)```/g;
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = codeBlockPattern.exec(text)) !== null) {
    if (match.index > lastIndex) {
      segments.push({ type: "text", content: text.slice(lastIndex, match.index) });
    }
    segments.push({ type: "code", content: match[2], lang: match[1] || undefined });
    lastIndex = match.index + match[0].length;
  }

  if (lastIndex < text.length) {
    segments.push({ type: "text", content: text.slice(lastIndex) });
  }

  if (segments.length === 1 && segments[0].type === "text") {
    return <>{renderInline(text)}</>;
  }

  return (
    <>
      {segments.map((segment, index) => {
        if (segment.type === "code") {
          return (
            <pre key={index} className="md-code-block">
              {segment.lang && <span className="md-code-lang">{segment.lang}</span>}
              <code>{segment.content.replace(/\n$/, "")}</code>
            </pre>
          );
        }
        return <span key={index}>{renderInline(segment.content)}</span>;
      })}
    </>
  );
}
