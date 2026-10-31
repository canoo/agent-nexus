// Bundled symbols: no external requests or host supplied asset paths.
export function identityKey(value = '') {
  const v = String(value).toLowerCase();
  return /claude|anthropic/.test(v) ? 'claude' : /gemini|google/.test(v) ? 'gemini' : /gpt|openai|chatgpt|codex/.test(v) ? 'chatgpt' : /copilot|microsoft/.test(v) ? 'copilot' : /perplexity/.test(v) ? 'perplexity' : /qwen|llama|meta/.test(v) ? 'ollama' : /ollama|local/.test(v) ? 'ollama' : 'model';
}
export function identity(value) {
  const badge = document.createElement('span'); badge.className = 'identity'; badge.title = /qwen|llama/i.test(String(value)) ? `${value} · served by Ollama` : String(value);
  const image = document.createElement('img'); image.src = `assets/${identityKey(value)}.svg`; image.alt = String(value); image.width = 28; image.height = 28;
  const label = document.createElement('span'); label.textContent = String(value); badge.append(image,label); return badge;
}
