// Ollama reference components: https://github.com/ollama/ollama/blob/main/types/model/name.go
export function isValidModelName(value) {
  if (typeof value !== 'string' || value.length === 0) {
    return false;
  }
  if (/[\s\x00-\x1f\x7f?#@]/.test(value)) {
    return false;
  }

  let rest = value;
  const hasScheme = rest.startsWith('http://') || rest.startsWith('https://');
  if (hasScheme) {
    if (rest.startsWith('http://')) {
      rest = rest.slice(7);
    } else {
      rest = rest.slice(8);
    }
  }

  const parts = rest.split('/');
  if (hasScheme && parts.length !== 3) {
    return false;
  }
  if (!hasScheme && (parts.length < 1 || parts.length > 3)) {
    return false;
  }

  const namePartRegex = /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,79}$/;
  const namespaceRegex = /^[A-Za-z0-9_][A-Za-z0-9_-]{0,79}$/;
  const registryRegex = /^[A-Za-z0-9_][A-Za-z0-9_.:-]{0,349}$/;

  const finalPart = parts[parts.length - 1];
  const tagIndex = finalPart.indexOf(':');
  let modelName = finalPart;
  let tag = null;

  if (tagIndex !== -1) {
    if (finalPart.indexOf(':', tagIndex + 1) !== -1) {
      return false;
    }
    modelName = finalPart.slice(0, tagIndex);
    tag = finalPart.slice(tagIndex + 1);
  }

  if (!namePartRegex.test(modelName)) {
    return false;
  }
  if (tag !== null && !namePartRegex.test(tag)) {
    return false;
  }

  if (parts.length === 2) {
    const namespacePart = parts[0];
    if (!namespaceRegex.test(namespacePart)) {
      return false;
    }
  } else if (parts.length === 3) {
    const registryPart = parts[0];
    const namespacePart = parts[1];
    if (!registryRegex.test(registryPart)) {
      return false;
    }
    if (!namespaceRegex.test(namespacePart)) {
      return false;
    }
  }

  return true;
}

const SETTING_KEYS = [
  'NEXUS_SUPERVISOR_MODEL',
  'NEXUS_LOGIC_MODEL',
  'NEXUS_MODEL_COMMIT_MSG',
  'NEXUS_MODEL_BOILERPLATE',
  'NEXUS_MODEL_TEST_SCAFFOLD',
  'NEXUS_MODEL_LINT_FIX',
  'NEXUS_MODEL_LOGIC_REFACTOR',
];

export function createModelRoutes(settings = {}) {
  for (const key of SETTING_KEYS) {
    if (Object.hasOwn(settings, key)) {
      const val = settings[key];
      if (!isValidModelName(val)) {
        throw new Error(`${key} must be a non-empty Ollama model name (for example qwen2.5-coder:1.5b).`);
      }
    }
  }

  const supervisorDefault = 'qwen2.5-coder:1.5b';
  const logicDefault = 'llama3.2:3b';

  const supervisorModel = Object.hasOwn(settings, 'NEXUS_SUPERVISOR_MODEL')
    ? settings.NEXUS_SUPERVISOR_MODEL
    : supervisorDefault;

  const logicModel = Object.hasOwn(settings, 'NEXUS_LOGIC_MODEL')
    ? settings.NEXUS_LOGIC_MODEL
    : logicDefault;

  return {
    'commit-msg': Object.hasOwn(settings, 'NEXUS_MODEL_COMMIT_MSG')
      ? settings.NEXUS_MODEL_COMMIT_MSG
      : supervisorModel,
    boilerplate: Object.hasOwn(settings, 'NEXUS_MODEL_BOILERPLATE')
      ? settings.NEXUS_MODEL_BOILERPLATE
      : supervisorModel,
    'test-scaffold': Object.hasOwn(settings, 'NEXUS_MODEL_TEST_SCAFFOLD')
      ? settings.NEXUS_MODEL_TEST_SCAFFOLD
      : supervisorModel,
    'lint-fix': Object.hasOwn(settings, 'NEXUS_MODEL_LINT_FIX')
      ? settings.NEXUS_MODEL_LINT_FIX
      : logicModel,
    'logic-refactor': Object.hasOwn(settings, 'NEXUS_MODEL_LOGIC_REFACTOR')
      ? settings.NEXUS_MODEL_LOGIC_REFACTOR
      : logicModel,
  };
}
