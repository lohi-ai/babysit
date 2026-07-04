import { renderMarkdown } from '../lib/md';

export function Markdown({ source }: { source: string }) {
  return (
    <div className="md" dangerouslySetInnerHTML={{ __html: renderMarkdown(source) }} />
  );
}
