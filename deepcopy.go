package mnemosyne

import (
	. "reflect"
	"runtime/debug"

	"github.com/mitchellh/mapstructure"
	"github.com/sirupsen/logrus"
)

// ShallowCopy is fast copy tool, to copy go structs and maps
// modified version of https://raw.githubusercontent.com/gavv/deepcopy/master/deepcopy.go
func ShallowCopy(src interface{}, dst interface{}) {

	srcv := ValueOf(src)
	dstv := ValueOf(dst)

	if dstv.Kind() == Ptr {
		dstv = dstv.Elem()
	}
	if srcv.Kind() == Ptr {
		srcv = srcv.Elem()
	}
	if srcv.Kind() == Map && dstv.Kind() == Struct {
		mapstructure.Decode(src, &dst)
		return
	}

	if srcv.Kind() != dstv.Kind() {
		logrus.Errorf("Diffrent object kinds, %v != %v", srcv.Kind(), dstv.Kind())
		debug.PrintStack()
		return
	}

	// Copy the elements
	// maybe we need to support array, slice later.
	switch srcv.Kind() {
	case Array, Slice:
		for i := 0; i < srcv.Len(); i++ {
			dstv.Index(i).Set(srcv.Index(i))
		}
		return
	case Map:
		for _, k := range srcv.MapKeys() {
			dstv.SetMapIndex(k, srcv.MapIndex(k))
		}
		return
	case Struct:
		for i, n := 0, srcv.NumField(); i < n; i++ {
			if dstv.Field(i).CanSet() {
				dstv.Field(i).Set(srcv.Field(i))
			}
		}
		return
	}

	logrus.Errorf("Unsupported kind, %v, %v", srcv.Kind(), dstv.Kind())
	debug.PrintStack()
}
