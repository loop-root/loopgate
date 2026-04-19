package secrets

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

static CFStringRef morphMakeCFString(const char *s) {
	return CFStringCreateWithCString(NULL, s, kCFStringEncodingUTF8);
}

static CFDataRef morphMakeCFData(const void *data, CFIndex len) {
	return CFDataCreate(NULL, (const UInt8 *)data, len);
}

static CFDictionaryRef morphMakeQueryDict(CFTypeRef svc, CFTypeRef acct) {
	const void *keys[]   = { kSecClass, kSecAttrService, kSecAttrAccount };
	const void *values[] = { kSecClassGenericPassword, svc, acct };
	return CFDictionaryCreate(NULL, keys, values, 3,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
}

static CFDictionaryRef morphMakeAddDict(CFTypeRef svc, CFTypeRef acct, CFTypeRef data) {
	const void *keys[]   = { kSecClass, kSecAttrService, kSecAttrAccount, kSecValueData };
	const void *values[] = { kSecClassGenericPassword, svc, acct, data };
	return CFDictionaryCreate(NULL, keys, values, 4,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
}

static CFDictionaryRef morphMakeUpdateDict(CFTypeRef data) {
	const void *keys[]   = { kSecValueData };
	const void *values[] = { data };
	return CFDictionaryCreate(NULL, keys, values, 1,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
}

static char *morphCopySecErrorMessage(OSStatus status) {
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
*/
import "C"

import (
	"context"
	"fmt"
	"unsafe"
)

func storeSecretInMacOSKeychain(_ context.Context, validatedRef SecretRef, rawSecret []byte) error {
	cService := C.CString(keychainServiceName(validatedRef))
	defer C.free(unsafe.Pointer(cService))
	cAccount := C.CString(validatedRef.AccountName)
	defer C.free(unsafe.Pointer(cAccount))

	serviceCF := C.morphMakeCFString(cService)
	defer C.CFRelease(C.CFTypeRef(serviceCF))
	accountCF := C.morphMakeCFString(cAccount)
	defer C.CFRelease(C.CFTypeRef(accountCF))

	var dataPtr unsafe.Pointer
	if len(rawSecret) > 0 {
		dataPtr = unsafe.Pointer(&rawSecret[0])
	}
	dataCF := C.morphMakeCFData(dataPtr, C.CFIndex(len(rawSecret)))
	defer C.CFRelease(C.CFTypeRef(dataCF))

	// Try add first.
	addDict := C.morphMakeAddDict(C.CFTypeRef(serviceCF), C.CFTypeRef(accountCF), C.CFTypeRef(dataCF))
	defer C.CFRelease(C.CFTypeRef(addDict))

	addStatus := C.SecItemAdd(C.CFDictionaryRef(addDict), nil)
	if addStatus == C.errSecSuccess {
		return nil
	}
	if addStatus != C.errSecDuplicateItem {
		return mapKeychainStatus("store secret", validatedRef, addStatus)
	}

	// Item already exists — update the data in place.
	queryDict := C.morphMakeQueryDict(C.CFTypeRef(serviceCF), C.CFTypeRef(accountCF))
	defer C.CFRelease(C.CFTypeRef(queryDict))
	updateDict := C.morphMakeUpdateDict(C.CFTypeRef(dataCF))
	defer C.CFRelease(C.CFTypeRef(updateDict))

	updateStatus := C.SecItemUpdate(C.CFDictionaryRef(queryDict), C.CFDictionaryRef(updateDict))
	if updateStatus != C.errSecSuccess {
		return mapKeychainStatus("store secret", validatedRef, updateStatus)
	}
	return nil
}

func mapKeychainStatus(operation string, validatedRef SecretRef, status C.OSStatus) error {
	if status == C.errSecItemNotFound {
		return fmt.Errorf("%w: keychain item for secret ref %q", ErrSecretNotFound, validatedRef.ID)
	}
	return formatKeychainStatusError(operation, validatedRef, int(status), keychainStatusMessage(status))
}

func keychainStatusMessage(status C.OSStatus) string {
	errorMessageCString := C.morphCopySecErrorMessage(status)
	if errorMessageCString == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(errorMessageCString))
	return C.GoString(errorMessageCString)
}
