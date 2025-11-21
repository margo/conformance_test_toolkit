package pointers

// Ptr returns a pointer to the given value of any type
func Ptr[T any](v T) *T {
	return &v
}

// PtrOrNil returns a pointer to the value if it's not the zero value, otherwise nil
func PtrOrNil[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// PtrSlice converts a slice of values to a slice of pointers
func PtrSlice[T any](slice []T) []*T {
	if slice == nil {
		return nil
	}
	result := make([]*T, len(slice))
	for i := range slice {
		result[i] = &slice[i]
	}
	return result
}

// ValueSlice converts a slice of pointers to a slice of values
func ValueSlice[T any](slice []*T) []T {
	if slice == nil {
		return nil
	}
	result := make([]T, 0, len(slice))
	for _, ptr := range slice {
		if ptr != nil {
			result = append(result, *ptr)
		}
	}
	return result
}

// Deref safely dereferences a pointer, returning the zero value if nil
func Deref[T any](ptr *T) T {
	if ptr == nil {
		var zero T
		return zero
	}
	return *ptr
}

// DerefOr safely dereferences a pointer, returning the default value if nil
func DerefOr[T any](ptr *T, defaultValue T) T {
	if ptr == nil {
		return defaultValue
	}
	return *ptr
}

// Equal compares two pointers, handling nil cases
func Equal[T comparable](a, b *T) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// Clone creates a new pointer with the same value (deep copy for the pointer itself)
func Clone[T any](ptr *T) *T {
	if ptr == nil {
		return nil
	}
	return Ptr(*ptr)
}
