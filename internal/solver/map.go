package solver

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
)

// Int64Map 是按 key 的 UTF-8 字节序（Go 字符串序）输出的 map，
// 保证“按 id 返回所有相对值”时 JSON 字段顺序确定，便于复核与快照比对。
type Int64Map map[string]int64

// MarshalJSON 以排序后的键序列化为 JSON 对象。
func (m Int64Map) MarshalJSON() ([]byte, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.WriteString(strconv.FormatInt(m[k], 10))
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
