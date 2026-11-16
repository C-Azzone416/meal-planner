//go:build darwin && cgo

package localclient

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation

#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

static void mp_zero_and_free(void *value, size_t length) {
	if (value == NULL) {
		return;
	}
	volatile unsigned char *cursor = (volatile unsigned char *)value;
	while (length-- > 0) {
		*cursor++ = 0;
	}
	free(value);
}

static CFMutableDictionaryRef mp_keychain_query(
	const char *service,
	size_t service_length,
	const char *account,
	size_t account_length
) {
	CFStringRef service_string = CFStringCreateWithBytes(
		kCFAllocatorDefault,
		(const UInt8 *)service,
		(CFIndex)service_length,
		kCFStringEncodingUTF8,
		false
	);
	CFStringRef account_string = CFStringCreateWithBytes(
		kCFAllocatorDefault,
		(const UInt8 *)account,
		(CFIndex)account_length,
		kCFStringEncodingUTF8,
		false
	);
	if (service_string == NULL || account_string == NULL) {
		if (service_string != NULL) CFRelease(service_string);
		if (account_string != NULL) CFRelease(account_string);
		return NULL;
	}

	CFMutableDictionaryRef query = CFDictionaryCreateMutable(
		kCFAllocatorDefault,
		0,
		&kCFTypeDictionaryKeyCallBacks,
		&kCFTypeDictionaryValueCallBacks
	);
	if (query != NULL) {
		CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword);
		CFDictionarySetValue(query, kSecAttrService, service_string);
		CFDictionarySetValue(query, kSecAttrAccount, account_string);
	}
	CFRelease(service_string);
	CFRelease(account_string);
	return query;
}

static OSStatus mp_keychain_get(
	const char *service,
	size_t service_length,
	const char *account,
	size_t account_length,
	void **output,
	size_t *output_length
) {
	*output = NULL;
	*output_length = 0;
	CFMutableDictionaryRef query = mp_keychain_query(
		service, service_length, account, account_length
	);
	if (query == NULL) return errSecAllocate;
	CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
	CFDictionarySetValue(query, kSecMatchLimit, kSecMatchLimitOne);

	CFTypeRef result = NULL;
	OSStatus status = SecItemCopyMatching(query, &result);
	CFRelease(query);
	if (status != errSecSuccess) return status;
	if (result == NULL || CFGetTypeID(result) != CFDataGetTypeID()) {
		if (result != NULL) CFRelease(result);
		return errSecDecode;
	}

	CFDataRef data = (CFDataRef)result;
	CFIndex length = CFDataGetLength(data);
	if (length <= 0) {
		CFRelease(result);
		return errSecDecode;
	}
	void *copy = malloc((size_t)length);
	if (copy == NULL) {
		CFRelease(result);
		return errSecAllocate;
	}
	memcpy(copy, CFDataGetBytePtr(data), (size_t)length);
	CFRelease(result);
	*output = copy;
	*output_length = (size_t)length;
	return errSecSuccess;
}

static OSStatus mp_keychain_put(
	const char *service,
	size_t service_length,
	const char *account,
	size_t account_length,
	const void *value,
	size_t value_length
) {
	CFDataRef data = CFDataCreate(
		kCFAllocatorDefault,
		(const UInt8 *)value,
		(CFIndex)value_length
	);
	if (data == NULL) return errSecAllocate;

	CFMutableDictionaryRef query = mp_keychain_query(
		service, service_length, account, account_length
	);
	CFMutableDictionaryRef attributes = CFDictionaryCreateMutable(
		kCFAllocatorDefault,
		0,
		&kCFTypeDictionaryKeyCallBacks,
		&kCFTypeDictionaryValueCallBacks
	);
	if (query == NULL || attributes == NULL) {
		if (query != NULL) CFRelease(query);
		if (attributes != NULL) CFRelease(attributes);
		CFRelease(data);
		return errSecAllocate;
	}
	CFDictionarySetValue(attributes, kSecValueData, data);
	CFDictionarySetValue(
		attributes,
		kSecAttrAccessible,
		kSecAttrAccessibleWhenUnlockedThisDeviceOnly
	);

	OSStatus status = SecItemUpdate(query, attributes);
	if (status == errSecItemNotFound) {
		CFMutableDictionaryRef item = mp_keychain_query(
			service, service_length, account, account_length
		);
		if (item == NULL) {
			status = errSecAllocate;
		} else {
			CFDictionarySetValue(item, kSecValueData, data);
			CFDictionarySetValue(
				item,
				kSecAttrAccessible,
				kSecAttrAccessibleWhenUnlockedThisDeviceOnly
			);
			status = SecItemAdd(item, NULL);
			CFRelease(item);
			// Another process may have inserted the same item between our
			// update and add. Resolve that race as an update, never as a
			// second item.
			if (status == errSecDuplicateItem) {
				status = SecItemUpdate(query, attributes);
			}
		}
	}

	CFRelease(attributes);
	CFRelease(query);
	CFRelease(data);
	return status;
}

static OSStatus mp_keychain_delete(
	const char *service,
	size_t service_length,
	const char *account,
	size_t account_length
) {
	CFMutableDictionaryRef query = mp_keychain_query(
		service, service_length, account, account_length
	);
	if (query == NULL) return errSecAllocate;
	OSStatus status = SecItemDelete(query);
	CFRelease(query);
	return status;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

var errNativeKeychain = errors.New("native keychain operation failed")

type nativeKeychainBackend struct{}

func newNativeKeychainBackend() (keychainBackend, error) {
	// Construction performs no SecItem operation. The shipped command does not
	// call this constructor while the source and environment gates are closed.
	return nativeKeychainBackend{}, nil
}

func (nativeKeychainBackend) get(service, account string) ([]byte, error) {
	serviceBytes := []byte(service)
	accountBytes := []byte(account)
	var output unsafe.Pointer
	var outputLength C.size_t
	status := C.mp_keychain_get(
		(*C.char)(unsafe.Pointer(&serviceBytes[0])), C.size_t(len(serviceBytes)),
		(*C.char)(unsafe.Pointer(&accountBytes[0])), C.size_t(len(accountBytes)),
		&output, &outputLength,
	)
	if output != nil {
		defer C.mp_zero_and_free(output, outputLength)
	}
	if status == C.errSecItemNotFound {
		return nil, errKeychainItemNotFound
	}
	if status != C.errSecSuccess || output == nil || outputLength == 0 || outputLength > 4096 {
		return nil, errNativeKeychain
	}
	return C.GoBytes(output, C.int(outputLength)), nil
}

func (nativeKeychainBackend) put(service, account string, value []byte) error {
	if len(value) == 0 {
		return errNativeKeychain
	}
	serviceBytes := []byte(service)
	accountBytes := []byte(account)
	secret := C.CBytes(value)
	defer C.mp_zero_and_free(secret, C.size_t(len(value)))
	status := C.mp_keychain_put(
		(*C.char)(unsafe.Pointer(&serviceBytes[0])), C.size_t(len(serviceBytes)),
		(*C.char)(unsafe.Pointer(&accountBytes[0])), C.size_t(len(accountBytes)),
		secret, C.size_t(len(value)),
	)
	if status != C.errSecSuccess {
		return errNativeKeychain
	}
	return nil
}

func (nativeKeychainBackend) delete(service, account string) error {
	serviceBytes := []byte(service)
	accountBytes := []byte(account)
	status := C.mp_keychain_delete(
		(*C.char)(unsafe.Pointer(&serviceBytes[0])), C.size_t(len(serviceBytes)),
		(*C.char)(unsafe.Pointer(&accountBytes[0])), C.size_t(len(accountBytes)),
	)
	if status == C.errSecItemNotFound {
		return errKeychainItemNotFound
	}
	if status != C.errSecSuccess {
		return errNativeKeychain
	}
	return nil
}
