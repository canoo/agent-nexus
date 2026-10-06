# Shared literal .env contract; source this helper, never source the data file.
_nexus_load_settings() {
    local env_file="$1" line key value original_keys='|'
    [ -f "$env_file" ] || return 0
    for key in OLLAMA_HOST_URL NEXUS_LOCAL_AI NEXUS_SUPERVISOR_MODEL NEXUS_LOGIC_MODEL NEXUS_MODEL_COMMIT_MSG NEXUS_MODEL_BOILERPLATE NEXUS_MODEL_TEST_SCAFFOLD NEXUS_MODEL_LINT_FIX NEXUS_MODEL_LOGIC_REFACTOR; do
        if declare -p "$key" >/dev/null 2>&1; then original_keys+="$key|"; fi
    done
    while IFS= read -r line || [ -n "$line" ]; do
        if [[ "$line" =~ ^[[:blank:]]*([A-Za-z_][A-Za-z0-9_]*)[[:blank:]]*=[[:blank:]]*(.*)$ ]]; then
            key="${BASH_REMATCH[1]}"; value="${BASH_REMATCH[2]}"
            case "$key" in
                OLLAMA_HOST_URL|NEXUS_LOCAL_AI|NEXUS_SUPERVISOR_MODEL|NEXUS_LOGIC_MODEL|NEXUS_MODEL_COMMIT_MSG|NEXUS_MODEL_BOILERPLATE|NEXUS_MODEL_TEST_SCAFFOLD|NEXUS_MODEL_LINT_FIX|NEXUS_MODEL_LOGIC_REFACTOR) ;;
                *) continue ;;
            esac
            [[ "$original_keys" == *"|$key|"* ]] && continue
            value="${value%$'\r'}"
            while [[ "$value" == *' ' || "$value" == *$'\t' ]]; do value="${value%?}"; done
            case "$value" in
                \"*\") value="${value:1:${#value}-2}" ;;
                \'*\') value="${value:1:${#value}-2}" ;;
            esac
            export "$key=$value"
        fi
    done < "$env_file"
}
