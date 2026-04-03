package loopgate

import (
	"errors"
	"fmt"
	"strings"
)

func SafeMemoryRememberErrorText(err error) string {
	var deniedError RequestDeniedError
	if errors.As(err, &deniedError) {
		switch deniedError.DenialCode {
		case DenialCodeMemoryCandidateDangerous:
			return fmt.Sprintf("explicit memory write was denied as unsafe and was not stored (%s)", deniedError.DenialCode)
		case DenialCodeMemoryCandidateInvalid:
			return fmt.Sprintf("explicit memory write could not be analyzed safely and was not stored (%s)", deniedError.DenialCode)
		case DenialCodeMemoryCandidateQuarantineRequired:
			return fmt.Sprintf("explicit memory write requires quarantine review and was not stored (%s)", deniedError.DenialCode)
		case DenialCodeMemoryCandidateReviewRequired:
			return fmt.Sprintf("explicit memory write requires review and was not stored (%s)", deniedError.DenialCode)
		case DenialCodeMemoryCandidateDropped:
			return fmt.Sprintf("explicit memory write was not retained and was not stored (%s)", deniedError.DenialCode)
		case DenialCodeAuditUnavailable:
			return fmt.Sprintf("explicit memory write could not be stored because audit persistence was unavailable (%s)", deniedError.DenialCode)
		default:
			if strings.TrimSpace(deniedError.DenialCode) != "" {
				return fmt.Sprintf("explicit memory write was denied and was not stored (%s)", deniedError.DenialCode)
			}
			return "explicit memory write was denied and was not stored"
		}
	}
	return "explicit memory write failed before it could be stored"
}
