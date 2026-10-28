package wifi

// Information element IDs (IEEE 802.11-2020, table 9-92) used by the decoder.
const (
	idSSID            = 0
	idSupportedRates  = 1
	idCountry         = 7
	idBSSLoad         = 11
	idHTCapabilities  = 45
	idRSN             = 48
	idExtendedRates   = 50
	idMobilityDomain  = 54
	idHTOperation     = 61
	idRMEnabledCaps   = 70
	idExtendedCaps    = 127
	idVHTCapabilities = 191
	idVHTOperation    = 192
	idVendor          = 221
	idExtension       = 255

	// Element ID extensions (valid when the element ID is idExtension).
	extHECapabilities  = 35
	extHEOperation     = 36
	extEHTOperation    = 106
	extEHTCapabilities = 108
)

// elementSet indexes the information elements of a beacon or probe response.
// Element IDs that may repeat (vendor specific) are kept as a list; for all
// others the first occurrence wins, so elements from the probe response
// take precedence over those from the separately captured beacon.
type elementSet struct {
	byKey  map[uint16][]byte
	vendor [][]byte
}

func elementKey(id, ext byte) uint16 { return uint16(id)<<8 | uint16(ext) }

// newElementSet parses one or more raw element blobs. Truncated trailing data
// is ignored.
func newElementSet(blobs ...[]byte) *elementSet {
	es := &elementSet{byKey: make(map[uint16][]byte)}
	for _, blob := range blobs {
		for b := blob; len(b) >= 2; {
			id, n := b[0], int(b[1])
			if len(b) < 2+n {
				break
			}
			body := b[2 : 2+n]
			b = b[2+n:]
			var ext byte
			if id == idExtension {
				if len(body) == 0 {
					continue
				}
				ext, body = body[0], body[1:]
			}
			if id == idVendor {
				es.vendor = append(es.vendor, body)
				continue
			}
			k := elementKey(id, ext)
			if _, seen := es.byKey[k]; !seen {
				es.byKey[k] = body
			}
		}
	}
	return es
}

// get returns the body of a regular element.
func (es *elementSet) get(id byte) ([]byte, bool) {
	b, ok := es.byKey[elementKey(id, 0)]
	return b, ok
}

// getExt returns the body (without the extension ID) of an extension element.
func (es *elementSet) getExt(ext byte) ([]byte, bool) {
	b, ok := es.byKey[elementKey(idExtension, ext)]
	return b, ok
}
