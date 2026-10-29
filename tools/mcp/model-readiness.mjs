import { ollamaFetch } from './settings.mjs';
import { isValidModelName } from './model-routes.mjs';

export async function ensureModelAvailable(settings, model, timeoutMs = 5000) {
  if (!isValidModelName(model)) {
    throw new Error('Ollama model availability check received an invalid model name.');
  }

  const hostUrl = settings.OLLAMA_HOST_URL || 'http://localhost:11434';
  let response;
  try {
    response = await ollamaFetch(settings, `${hostUrl}/api/show`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({ model }),
      signal: AbortSignal.timeout(timeoutMs),
      redirect: 'error'
    });
  } catch (error) {
    if (settings.NEXUS_LOCAL_AI === 'false') {
      throw error;
    }
    throw new Error('Ollama model availability check failed or timed out; verify the configured Ollama instance.');
  }

  try {
    await response.body?.cancel();
  } catch (_) {}

  if (response.status === 404) {
    throw new Error(`Ollama model "${model}" is not installed on the configured Ollama instance. Run ollama pull '${model}' on that instance, or change the NEXUS model setting.`);
  }

  if (response.status !== 200) {
    throw new Error(`Ollama model availability check returned HTTP ${response.status}; verify the configured Ollama instance.`);
  }

  return undefined;
}
