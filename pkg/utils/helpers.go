// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2023 Nordix Foundation.

// Package utils has some utility functions and interfaces
package utils

import (
	"go.einride.tech/aip/fieldmask"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// ApplyMaskToStoredPbObject updates the stored PB object with the one provided
// in the update grpc request based on the provided field mask
func ApplyMaskToStoredPbObject[T proto.Message](updateMask *fieldmaskpb.FieldMask, dst, src T) {
	fieldmask.Update(updateMask, dst, src)
}
