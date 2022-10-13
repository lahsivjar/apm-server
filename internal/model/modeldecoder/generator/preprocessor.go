// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package generator

import (
	"fmt"
	"io"
	"strings"
)

func generateFlattenAndMergeCode(w io.Writer, f structField, src structField) error {
	srcJSONKey, ok := src.tag.Lookup("json")
	if !ok {
		srcJSONKey = src.Name()
	}
	fieldJSONKey, ok := f.tag.Lookup("json")
	if !ok {
		fieldJSONKey = f.Name()
	}

	if !strings.HasPrefix(fieldJSONKey, fmt.Sprintf("%s.", srcJSONKey)) {
		// The current field cannot be set from the source map
		return nil
	}
	srcMapName := src.Name()
	targetFieldName := f.Name()
	targetMapKey := string(fieldJSONKey[len(srcJSONKey)+1:])
	fmt.Fprintf(w, `
	if rawFieldVal, ok := val.%s["%s"]; ok {
		fieldVal, ok := rawFieldVal.(string)
		if !ok {
			return errors.New("invalid data")
		}
		val.%s.Set(fieldVal)
		delete(val.%s, "%s")
	}
`[1:], srcMapName, targetMapKey, targetFieldName, srcMapName, targetMapKey)
	return nil
}
