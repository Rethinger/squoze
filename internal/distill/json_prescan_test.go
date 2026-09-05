package distill

import (
	"strings"
	"testing"
)

// prescanCase is one JSON shape plus what the pre-scan is expected to conclude
// about it. wantGate is the claim under test in TestPrescanRejectsDeadWork:
// false means "this input cannot be improved, and the gate must say so before
// paying for json.Unmarshal".
type prescanCase struct {
	name     string
	body     string
	wantGate bool
}

func prescanCases() []prescanCase {
	return []prescanCase{
		{"root_array_of_objects", rowsArray(30, "@root"), true},
		{"allowlist_data", wrapped("data", 30), true},
		{"structural_rows", wrapped("rows", 30), true},
		{"structural_dotted_key", wrapped("v1.rows", 30), true},
		{"nested_depth2", `{"result":{"status":"ok","rows":` + rowsArray(30, "n") + `}}`, true},
		{"nested_depth3_unreachable", `{"a":{"b":{"rows":` + rowsArray(30, "n") + `}}}`, false},
		{"two_arrays_would_lose_one", `{"rows":` + rowsArray(30, "r") + `,"logs":` + rowsArray(4, "l") + `}`, false},
		{"object_sibling_would_be_dropped", `{"rows":` + rowsArray(30, "r") + `,"meta":{"page":1}}`, false},
		{"empty_sibling_is_no_loss", `{"rows":` + rowsArray(30, "r") + `,"warnings":[]}`, true},
		{"metadata_sibling_is_no_loss", `{"rows":` + rowsArray(30, "r") + `,"_links":{"self":"/x"}}`, true},
		{"nulls_present", `{"id":"abc123","label":"a long enough label to clear the 64 byte floor","note":null}`, true},
		{"metadata_key", `{"id":"abc123","label":"a long enough label to clear the 64 byte floor","etag":"W/x"}`, true},
		{"metadata_key_mixed_case", `{"id":"abc123","label":"a long enough label to clear the 64 byte floor","ETag":"W/x"}`, true},
		{"empty_collection_compact", `{"id":"abc123","label":"a long enough label to clear the floor","tags":[]}`, true},
		{"empty_collection_pretty", "{\n  \"id\": \"abc123\",\n  \"label\": \"long enough label to clear the 64 byte floor\",\n  \"tags\": [\n  ]\n}", true},
		{"nothing_to_do", `{"id":"abc123","kind":"widget","label":"a long enough label to clear the 64 byte floor","count":7}`, false},
		{"two_element_array", `{"items":[{"id":1,"name":"first widget here"},{"id":2,"name":"second widget"}],"kind":"short"}`, false},
		{"array_of_scalars", `{"ids":[1,2,3,4,5,6,7,8,9,10,11,12],"kind":"a long enough label to clear the floor"}`, false},
		{"array_of_strings_root", `["alpha","beta","gamma","delta","epsilon","zeta","eta","theta","iota","kappa"]`, false},
		{"first_element_not_object", `{"items":["alpha",{"id":1,"name":"x"},{"id":2,"name":"y"}],"kind":"mixed array here"}`, false},
		{"null_only_inside_string", `{"id":"abc123","label":"the error said null pointer, which is prose","count":7}`, true},
		{"invalid_json", `{"id":"abc123","label":"a long enough label to clear the 64 byte floor",`, false},
	}
}

// rowsArray renders an array of homogeneous objects, tag distinguishing the
// fixtures so a diff points at the right case.
func rowsArray(n int, tag string) string {
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":`)
		b.WriteString(itoa(i))
		b.WriteString(`,"name":"widget ` + tag + " " + itoa(i) + `","status":"active","qty":`)
		b.WriteString(itoa(i * 3))
		b.WriteString("}")
	}
	b.WriteString("]")
	return b.String()
}

func wrapped(key string, n int) string {
	return `{"object":"list","has_more":false,"` + key + `":` + rowsArray(n, key) + `}`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// TestPrescanIsOutputNeutral is the contract the pre-scan lives or dies by: for
// every input, the answer with the gate must be byte-identical to the answer
// without it. A gate that merely makes things faster is worthless if it also
// makes them different — the engine's cache-safe promise is that identical
// original bytes always produce identical output, and a version that quietly
// stops compressing some shape breaks every prompt-cache prefix built on it.
func TestPrescanIsOutputNeutral(t *testing.T) {
	for _, c := range prescanCases() {
		gated, gatedOK := distillJSON(c.body, true)
		full, fullOK := distillJSON(c.body, false)
		if gatedOK != fullOK {
			t.Errorf("%s: ok differs — gated %v, ungated %v", c.name, gatedOK, fullOK)
			continue
		}
		if gated != full {
			t.Errorf("%s: OUTPUT DIFFERS with the pre-scan (%d bytes) and without it (%d bytes)",
				c.name, len(gated), len(full))
		}
	}
}

// TestPrescanRejectsDeadWork keeps the neutrality test above from being
// vacuous. Neutrality is trivially satisfied by a gate that never fires; what
// makes it worth having is that it fires on exactly the inputs that used to cost
// milliseconds and return them unchanged.
func TestPrescanRejectsDeadWork(t *testing.T) {
	for _, c := range prescanCases() {
		if got := CanDistillJSON(strings.TrimSpace(c.body)); got != c.wantGate {
			t.Errorf("%s: CanDistillJSON = %v, want %v", c.name, got, c.wantGate)
		}
	}
}

// TestStructuralSearchFindsUnlistedKeys is the other half of the change: v0.3.0
// only lifted arrays under items/data/results/records, so a provider that names
// its list rows or files paid the full parse and got nothing back. The wrapper
// names stay first in preference order, so a document that lifted before still
// lifts from the same key.
func TestStructuralSearchFindsUnlistedKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"unlisted_key", wrapped("rows", 30), "rows"},
		{"depth_two", `{"result":{"rows":` + rowsArray(30, "n") + `}}`, "result.rows"},
		{"root", rowsArray(30, "root"), ""},
	} {
		keys, ok := FindLiftableArray(tc.body)
		if !ok {
			t.Errorf("%s: no liftable array found", tc.name)
			continue
		}
		if got := arrayLabel(keys); got != tc.want {
			t.Errorf("%s: array path = %q, want %q", tc.name, got, tc.want)
		}
		out, lifted := DistillJSON(tc.body)
		if !lifted {
			t.Errorf("%s: located %q but did not lift", tc.name, arrayLabel(keys))
			continue
		}
		if !strings.HasPrefix(out, "[... squoze table:") {
			t.Errorf("%s: lifted output is not a table headline: %.60s", tc.name, out)
		}
	}
}

// TestDottedKeyPathResolves guards the gjson escaping. A provider that names a
// key "v1.rows" would otherwise send the column-order lookup to a path that
// resolves to nothing, and the table would silently fall back to sorted columns.
func TestDottedKeyPathResolves(t *testing.T) {
	// Reverse-alphabetical columns, so document order and the sorted fallback
	// cannot be confused for one another: the header proves which one was used.
	body := `{"object":"list","v1.rows":` + reorderedRows(80) + `}`
	keys, ok := FindLiftableArray(body)
	if !ok || len(keys) != 1 || keys[0] != "v1.rows" {
		t.Fatalf("FindLiftableArray = %v, %v; want [v1.rows], true", keys, ok)
	}
	out, lifted := DistillJSON(body)
	if !lifted {
		t.Fatal("dotted key did not lift")
	}
	if !strings.Contains(out, "| quantity | product_name | identifier |") {
		t.Errorf("columns fell back to sorted order, so the escaped path did not resolve:\n%.200s", out)
	}
}

// TestWrapperNamesStillLift is the backward-compatibility line. v0.3.0 knew four
// wrapper names by heart; the search is structural now, and these four have to go
// on lifting from exactly the key they always did.
func TestWrapperNamesStillLift(t *testing.T) {
	for _, key := range []string{"items", "data", "results", "records"} {
		body := wrapped(key, 30)
		keys, ok := FindLiftableArray(body)
		if !ok || arrayLabel(keys) != key {
			t.Errorf("%s: FindLiftableArray = %v, %v; want [%s], true", key, keys, ok, key)
			continue
		}
		out, lifted := DistillJSON(body)
		if !lifted {
			t.Errorf("%s: did not lift", key)
			continue
		}
		if !strings.Contains(out, `from "`+key+`"`) {
			t.Errorf("%s: headline does not name the source key:\n%.120s", key, out)
		}
	}
}

// TestLiftDeclinedWhenSiblingWouldVanish is the fidelity half of the structural
// search. A table renders the array and nothing else, so a response carrying a
// second array or a meta object beside the rows must not be lifted: the sibling
// would be gone from what the model sees, with nothing in the output to say so.
// Declining costs savings; lifting would cost information.
func TestLiftDeclinedWhenSiblingWouldVanish(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		mustSee string
	}{
		{"second_array", `{"rows":` + rowsArray(30, "r") + `,"logs":` + rowsArray(4, "l") + `}`, "logs"},
		{"object_sibling", `{"rows":` + rowsArray(30, "r") + `,"meta":{"page":1}}`, "meta"},
		{"ninth_scalar_over_headline_budget",
			`{"a":1,"b":2,"c":3,"d":4,"e":5,"f":6,"g":7,"h":8,"i":9,"rows":` + rowsArray(30, "r") + `}`, `"i":9`},
	} {
		if _, ok := FindLiftableArray(tc.body); ok {
			t.Errorf("%s: lift accepted, but it would drop %s", tc.name, tc.mustSee)
		}
		out, _ := DistillJSON(tc.body)
		if !strings.Contains(out, tc.mustSee) {
			t.Errorf("%s: %s is missing from the output — the sibling was lost:\n%.200s",
				tc.name, tc.mustSee, out)
		}
	}
}

// reorderedRows renders rows whose keys are declared in reverse-alphabetical
// order, with every column varying so none is hoisted out of the table.
func reorderedRows(n int) string {
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"quantity":` + itoa(i*3) + `,"product_name":"widget ` + itoa(i) + `","identifier":` + itoa(i) + `}`)
	}
	b.WriteString("]")
	return b.String()
}
