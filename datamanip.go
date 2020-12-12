package mnemosyne

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type cachable struct {
	Time         time.Time
	CachedObject interface{}
}

type cachableRet struct {
	Time         time.Time
	CachedObject *json.RawMessage
}

func finalizeCacheResponse(rawBytes []byte, compress bool, refrence interface{}) (*cachable, error) {
	var finalBytes []byte
	if compress {
		finalBytes = decompressZlib(rawBytes)
	} else {
		finalBytes = rawBytes
	}
	var unMarshaledWithoutRefrence cachableRet
	unmarshalErr := json.Unmarshal(finalBytes, &unMarshaledWithoutRefrence)
	if unmarshalErr != nil {
		return nil, fmt.Errorf("failed to unmarshall cached value : %w", unmarshalErr)
	}

	if refrence != nil {
		unmarshalErr = json.Unmarshal(*unMarshaledWithoutRefrence.CachedObject, refrence)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("failed to unmarshall cached refrence value : %w", unmarshalErr)
		}
	}

	return &cachable{
		Time:         unMarshaledWithoutRefrence.Time,
		CachedObject: refrence,
	}, nil
}

func prepareCachePayload(value interface{}, compress bool) (finalData []byte, prepError error) {
	defer func() {
		if r := recover(); r != nil {
			//json.Marshal panics under heavy-load which is not repeated with the same values
			prepError = fmt.Errorf("panic in cache-set: %v", r)
		}
	}()
	rawData, err := json.Marshal(value)
	if err != nil {
		prepError = err
		return
	}
	if compress {
		finalData = compressZlib(rawData)
	} else {
		finalData = rawData
	}
	return
}

func compressZlib(input []byte) []byte {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	w.Write(input)
	w.Close()
	compressed := buf.Bytes()
	return compressed
}

func decompressZlib(input []byte) []byte {
	var out bytes.Buffer
	r, _ := zlib.NewReader(bytes.NewBuffer(input))
	io.Copy(&out, r)
	r.Close()
	original := out.Bytes()
	return original
}
