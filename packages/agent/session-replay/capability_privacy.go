package sessionreplay

// SanitizeCapabilityTransientPayload removes only structured transport keys;
// user-authored strings are preserved verbatim.
func SanitizeCapabilityTransientPayload(value map[string]any) map[string]any {
	return sanitizeCapabilityValue(value).(map[string]any)
}

func sanitizeCapabilityValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(current))
		for key, child := range current {
			switch key {
			case "turnCapabilityInvocation", "explicitSession", "consent", "authorization", "AuthorizeCodexNativeComputerUse":
				continue
			}
			result[key] = sanitizeCapabilityValue(child)
		}
		return result
	case []any:
		result := make([]any, len(current))
		for index, child := range current {
			result[index] = sanitizeCapabilityValue(child)
		}
		return result
	default:
		return value
	}
}
