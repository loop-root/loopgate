package secrets

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

static CFStringRef loopgateMakeCFString(const char *s) {
	return CFStringCreateWithCString(NULL, s, kCFStringEncodingUTF8);
}

static CFDataRef loopgateMakeCFData(const void *data, CFIndex len) {
	return CFDataCreate(NULL, (const UInt8 *)data, len);
}

static CFMutableDictionaryRef loopgateMakeQueryDict(CFTypeRef svc, CFTypeRef acct) {
	CFMutableDictionaryRef dict = CFDictionaryCreateMutable(NULL, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	if (dict == NULL) {
		return NULL;
	}
	CFDictionarySetValue(dict, kSecClass, kSecClassGenericPassword);
	CFDictionarySetValue(dict, kSecAttrService, svc);
	CFDictionarySetValue(dict, kSecAttrAccount, acct);
	return dict;
}

static CFDictionaryRef loopgateMakeAddDict(CFTypeRef svc, CFTypeRef acct, CFTypeRef data) {
	const void *keys[]   = { kSecClass, kSecAttrService, kSecAttrAccount, kSecValueData };
	const void *values[] = { kSecClassGenericPassword, svc, acct, data };
	return CFDictionaryCreate(NULL, keys, values, 4,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
}

static CFDictionaryRef loopgateMakeUpdateDict(CFTypeRef data) {
	const void *keys[]   = { kSecValueData };
	const void *values[] = { data };
	return CFDictionaryCreate(NULL, keys, values, 1,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
}

static char *loopgateCopySecErrorMessage(OSStatus status) {
	CFStringRef message = SecCopyErrorMessageString(status, NULL);
	if (message == NULL) {
		return NULL;
	}

	CFIndex length = CFStringGetLength(message);
	CFIndex maxSize = CFStringGetMaximumSizeForEncoding(length, kCFStringEncodingUTF8) + 1;
	char *buffer = (char *)malloc((size_t)maxSize);
	if (buffer == NULL) {
		CFRelease(message);
		return NULL;
	}
	if (!CFStringGetCString(message, buffer, maxSize, kCFStringEncodingUTF8)) {
		free(buffer);
		CFRelease(message);
		return NULL;
	}
	CFRelease(message);
	return buffer;
}

static OSStatus loopgateCopySecretData(CFTypeRef svc, CFTypeRef acct, CFDataRef *dataOut) {
	CFMutableDictionaryRef queryDict = loopgateMakeQueryDict(svc, acct);
	if (queryDict == NULL) {
		return errSecAllocate;
	}
	CFDictionarySetValue(queryDict, kSecReturnData, kCFBooleanTrue);
	CFDictionarySetValue(queryDict, kSecMatchLimit, kSecMatchLimitOne);
	OSStatus status = SecItemCopyMatching(queryDict, (CFTypeRef *)dataOut);
	CFRelease(queryDict);
	return status;
}

static OSStatus loopgateCopySecretMetadata(CFTypeRef svc, CFTypeRef acct, CFDictionaryRef *metadataOut) {
	CFMutableDictionaryRef queryDict = loopgateMakeQueryDict(svc, acct);
	if (queryDict == NULL) {
		return errSecAllocate;
	}
	CFDictionarySetValue(queryDict, kSecReturnAttributes, kCFBooleanTrue);
	CFDictionarySetValue(queryDict, kSecMatchLimit, kSecMatchLimitOne);
	OSStatus status = SecItemCopyMatching(queryDict, (CFTypeRef *)metadataOut);
	CFRelease(queryDict);
	return status;
}

static OSStatus loopgateDeleteSecret(CFTypeRef svc, CFTypeRef acct) {
	CFMutableDictionaryRef queryDict = loopgateMakeQueryDict(svc, acct);
	if (queryDict == NULL) {
		return errSecAllocate;
	}
	OSStatus status = SecItemDelete(queryDict);
	CFRelease(queryDict);
	return status;
}
*/
import "C"

import (
	"context"
	"fmt"
	"unsafe"
)

func storeSecretInMacOSKeychain(ctx context.Context, validatedRef SecretRef, serviceName string, rawSecret []byte) error {
	_ = ctx
	cService := C.CString(serviceName)
	defer C.free(unsafe.Pointer(cService))
	cAccount := C.CString(validatedRef.AccountName)
	defer C.free(unsafe.Pointer(cAccount))

	serviceCF := C.loopgateMakeCFString(cService)
	defer C.CFRelease(C.CFTypeRef(serviceCF))
	accountCF := C.loopgateMakeCFString(cAccount)
	defer C.CFRelease(C.CFTypeRef(accountCF))

	var dataPtr unsafe.Pointer
	if len(rawSecret) > 0 {
		dataPtr = unsafe.Pointer(&rawSecret[0])
	}
	dataCF := C.loopgateMakeCFData(dataPtr, C.CFIndex(len(rawSecret)))
	defer C.CFRelease(C.CFTypeRef(dataCF))

	// Try add first.
	addDict := C.loopgateMakeAddDict(C.CFTypeRef(serviceCF), C.CFTypeRef(accountCF), C.CFTypeRef(dataCF))
	defer C.CFRelease(C.CFTypeRef(addDict))

	addStatus := C.SecItemAdd(C.CFDictionaryRef(addDict), nil)
	if addStatus == C.errSecSuccess {
		return nil
	}
	if addStatus != C.errSecDuplicateItem {
		return mapKeychainStatus("store secret", validatedRef, addStatus)
	}

	// Item already exists — update the data in place.
	queryDict := C.loopgateMakeQueryDict(C.CFTypeRef(serviceCF), C.CFTypeRef(accountCF))
	defer C.CFRelease(C.CFTypeRef(queryDict))
	updateDict := C.loopgateMakeUpdateDict(C.CFTypeRef(dataCF))
	defer C.CFRelease(C.CFTypeRef(updateDict))

	updateStatus := C.SecItemUpdate(C.CFDictionaryRef(queryDict), C.CFDictionaryRef(updateDict))
	if updateStatus != C.errSecSuccess {
		return mapKeychainStatus("store secret", validatedRef, updateStatus)
	}
	return nil
}

func readSecretFromMacOSKeychain(ctx context.Context, validatedRef SecretRef, serviceName string) ([]byte, error) {
	_ = ctx
	cService := C.CString(serviceName)
	defer C.free(unsafe.Pointer(cService))
	cAccount := C.CString(validatedRef.AccountName)
	defer C.free(unsafe.Pointer(cAccount))

	serviceCF := C.loopgateMakeCFString(cService)
	defer C.CFRelease(C.CFTypeRef(serviceCF))
	accountCF := C.loopgateMakeCFString(cAccount)
	defer C.CFRelease(C.CFTypeRef(accountCF))

	var secretDataCF C.CFDataRef
	readStatus := C.loopgateCopySecretData(C.CFTypeRef(serviceCF), C.CFTypeRef(accountCF), &secretDataCF)
	if readStatus != C.errSecSuccess {
		return nil, mapKeychainStatus("read secret", validatedRef, readStatus)
	}
	defer C.CFRelease(C.CFTypeRef(secretDataCF))

	secretLength := C.CFDataGetLength(secretDataCF)
	if secretLength == 0 {
		return []byte{}, nil
	}
	secretBytesPtr := C.CFDataGetBytePtr(secretDataCF)
	return C.GoBytes(unsafe.Pointer(secretBytesPtr), C.int(secretLength)), nil
}

func deleteSecretFromMacOSKeychain(ctx context.Context, validatedRef SecretRef, serviceName string) error {
	_ = ctx
	cService := C.CString(serviceName)
	defer C.free(unsafe.Pointer(cService))
	cAccount := C.CString(validatedRef.AccountName)
	defer C.free(unsafe.Pointer(cAccount))

	serviceCF := C.loopgateMakeCFString(cService)
	defer C.CFRelease(C.CFTypeRef(serviceCF))
	accountCF := C.loopgateMakeCFString(cAccount)
	defer C.CFRelease(C.CFTypeRef(accountCF))

	deleteStatus := C.loopgateDeleteSecret(C.CFTypeRef(serviceCF), C.CFTypeRef(accountCF))
	if deleteStatus != C.errSecSuccess {
		return mapKeychainStatus("delete secret", validatedRef, deleteStatus)
	}
	return nil
}

func metadataForMacOSKeychainSecret(ctx context.Context, validatedRef SecretRef, serviceName string) (SecretMetadata, error) {
	_ = ctx
	cService := C.CString(serviceName)
	defer C.free(unsafe.Pointer(cService))
	cAccount := C.CString(validatedRef.AccountName)
	defer C.free(unsafe.Pointer(cAccount))

	serviceCF := C.loopgateMakeCFString(cService)
	defer C.CFRelease(C.CFTypeRef(serviceCF))
	accountCF := C.loopgateMakeCFString(cAccount)
	defer C.CFRelease(C.CFTypeRef(accountCF))

	var metadataCF C.CFDictionaryRef
	readStatus := C.loopgateCopySecretMetadata(C.CFTypeRef(serviceCF), C.CFTypeRef(accountCF), &metadataCF)
	if readStatus != C.errSecSuccess {
		return SecretMetadata{}, mapKeychainStatus("read secret metadata", validatedRef, readStatus)
	}
	defer C.CFRelease(C.CFTypeRef(metadataCF))

	return SecretMetadata{
		Status: "stored",
		Scope:  validatedRef.Scope,
	}, nil
}

func mapKeychainStatus(operation string, validatedRef SecretRef, status C.OSStatus) error {
	if status == C.errSecItemNotFound {
		return fmt.Errorf("%w: keychain item for secret ref %q", ErrSecretNotFound, validatedRef.ID)
	}
	return formatKeychainStatusError(operation, validatedRef, int(status), keychainStatusMessage(status))
}

func keychainStatusMessage(status C.OSStatus) string {
	errorMessageCString := C.loopgateCopySecErrorMessage(status)
	if errorMessageCString == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(errorMessageCString))
	return C.GoString(errorMessageCString)
}
