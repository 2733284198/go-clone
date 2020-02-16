package clone

import (
	"fmt"
	"reflect"
	"sync"
	"time"
	"unsafe"
)

var (
	cachedStructTypes sync.Map
)

func init() {
	// Some well-known scala-like structs.
	MarkAsScala(reflect.TypeOf(time.Time{}))
	MarkAsScala(reflect.TypeOf(reflect.Value{}))
}

// MarkAsScala marks t as a scala type so that all clone methods will copy t by value.
// If t is not struct or pointer to struct, MarkAsScala ignores t.
//
// In the most cases, it's not necessary to call it explicitly.
// If a struct type contains scala type fields only, the struct will be marked as scala automatically.
//
// Here is a list of types marked as scala by default:
//     * time.Time
//     * reflect.Value
func MarkAsScala(t reflect.Type) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return
	}

	cachedStructTypes.Store(t, structType{})
}

type structType struct {
	PointerFields []structFieldType
}

type structFieldType struct {
	Offset uintptr // The offset from the beginning of the struct.
	Index  int     // The index of the field.
}

func loadStructType(t reflect.Type) (st structType) {
	if v, ok := cachedStructTypes.Load(t); ok {
		st = v.(structType)
		return
	}

	num := t.NumField()
	pointerFields := make([]structFieldType, 0, num)

	for i := 0; i < num; i++ {
		field := t.Field(i)
		ft := field.Type
		k := ft.Kind()

		if isScala(k) {
			continue
		}

		switch k {
		case reflect.Array:
			if ft.Len() == 0 {
				continue
			}

			elem := ft.Elem()

			if isScala(elem.Kind()) {
				continue
			}

			if elem.Kind() == reflect.Struct {
				fst := loadStructType(elem)

				if len(fst.PointerFields) == 0 {
					continue
				}
			}
		case reflect.Struct:
			fst := loadStructType(ft)

			if len(fst.PointerFields) == 0 {
				continue
			}
		}

		pointerFields = append(pointerFields, structFieldType{
			Offset: field.Offset,
			Index:  i,
		})
	}

	if len(pointerFields) == 0 {
		pointerFields = nil // Release memory ASAP.
	}

	st = structType{
		PointerFields: pointerFields,
	}
	cachedStructTypes.LoadOrStore(t, st)
	return
}

func isScala(k reflect.Kind) bool {
	switch k {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128,
		reflect.String, reflect.Func,
		reflect.UnsafePointer:
		return true
	}

	return false
}

type baitType struct{}

func (baitType) Foo() {}

var (
	baitMethodValue = reflect.ValueOf(baitType{}).MethodByName("Foo")
)

func copyScalaValue(src reflect.Value) reflect.Value {
	if src.CanInterface() {
		return src
	}

	// src is an unexported field value. Copy its value.
	switch src.Kind() {
	case reflect.Bool:
		return reflect.ValueOf(src.Bool())

	case reflect.Int:
		return reflect.ValueOf(int(src.Int()))
	case reflect.Int8:
		return reflect.ValueOf(int8(src.Int()))
	case reflect.Int16:
		return reflect.ValueOf(int16(src.Int()))
	case reflect.Int32:
		return reflect.ValueOf(int32(src.Int()))
	case reflect.Int64:
		return reflect.ValueOf(src.Int())

	case reflect.Uint:
		return reflect.ValueOf(uint(src.Uint()))
	case reflect.Uint8:
		return reflect.ValueOf(uint8(src.Uint()))
	case reflect.Uint16:
		return reflect.ValueOf(uint16(src.Uint()))
	case reflect.Uint32:
		return reflect.ValueOf(uint32(src.Uint()))
	case reflect.Uint64:
		return reflect.ValueOf(src.Uint())
	case reflect.Uintptr:
		return reflect.ValueOf(uintptr(src.Uint()))

	case reflect.Float32:
		return reflect.ValueOf(float32(src.Float()))
	case reflect.Float64:
		return reflect.ValueOf(src.Float())

	case reflect.Complex64:
		return reflect.ValueOf(complex64(src.Complex()))
	case reflect.Complex128:
		return reflect.ValueOf(src.Complex())

	case reflect.String:
		return reflect.ValueOf(src.String())
	case reflect.Func:
		t := src.Type()

		if src.IsNil() {
			return reflect.Zero(t)
		}

		ptr := src.Pointer()

		// All methods return same pointer which is not useful at all.
		// In this case, the only choice is to give up and return a nil func.
		if ptr == baitMethodValue.Pointer() {
			return reflect.Zero(t)
		}

		// src.Pointer is the PC address of a func.
		pc := reflect.New(reflect.TypeOf(uintptr(0)))
		pc.Elem().SetUint(uint64(uintptr(ptr)))

		fn := reflect.New(src.Type())
		*(*uintptr)(unsafe.Pointer(fn.Pointer())) = pc.Pointer()
		return fn.Elem()
	case reflect.UnsafePointer:
		return reflect.ValueOf(unsafe.Pointer(src.Pointer()))
	}

	panic(fmt.Errorf("go-clone: <bug> impossible type `%v` when cloning private field", src.Type()))
}