package lib

import (
	"bytes"
	"log"

	"github.com/clbanning/mxj"
)

var (
	elementMessage       = []byte("<message")
	elementDelProperty   = []byte("<delProperty")
	elementGetProperties = []byte("<getProperties")

	elementSingleTagEnd = []byte("/>")
)

// XmlFlattener reads XML from INDI-server by chunks and returns elements
type XmlFlattener struct {
	buffer         []byte
	nextEndElement []byte
}

func init() {
	mxj.PrependAttrWithHyphen(false)
	mxj.SetAttrPrefix("attr_")
	mxj.DecodeSimpleValuesAsMap(false)
}

// NewXMLReader returns XmlReader
func NewXmlFlattener() *XmlFlattener {
	return &XmlFlattener{
		buffer: make([]byte, 0, INDIServerMaxRecvMsgSize),
	}
}

func (r *XmlFlattener) FeedChunk(chunk []byte) [][]byte {
	if len(chunk) == 0 {
		return nil
	}

	elements := make([][]byte, 0, 10)

	r.buffer = append(r.buffer, chunk...)

	for {
		if len(r.nextEndElement) == 0 {
			// look for start of new entity
			if n := bytes.IndexByte(r.buffer, '<'); n > 0 {
				r.buffer = r.buffer[n:]
			}

			if r.buffer[0] != '<' {
				// something went wrong
				return elements
			}

			// special case of message-element without closing tag
			if bytes.HasPrefix(r.buffer, elementMessage) || bytes.HasPrefix(r.buffer, elementDelProperty) ||
				bytes.HasPrefix(r.buffer, elementGetProperties) {
				r.nextEndElement = elementSingleTagEnd
			} else {
				// get next end element value
				if n := bytes.IndexByte(r.buffer, ' '); n < 1 {
					// something went wrong
					return elements
				} else {
					r.nextEndElement = []byte{'<', '/'}
					if r.buffer[n-1] == '\n' {
						n = n - 1
					}
					r.nextEndElement = append(r.nextEndElement, r.buffer[1:n]...)
					r.nextEndElement = append(r.nextEndElement, '>')
				}
			}
		}

		if n := bytes.Index(r.buffer, r.nextEndElement); n == -1 {
			// should read next chunk
			return elements
		} else {
			// element closed, add it to return
			end := n + len(r.nextEndElement) + 1
			if end > len(r.buffer) {
				end = len(r.buffer)
			}
			elements = append(elements, r.buffer[:end])
			r.nextEndElement = r.nextEndElement[:0]
			if len(r.buffer) == end {
				r.buffer = r.buffer[:0]
				break
			}

			r.buffer = r.buffer[end:]
		}
	}

	return elements
}

func (r *XmlFlattener) ConvertChunkToJSON(chunk []byte) [][]byte {
	elements := r.FeedChunk(chunk)
	if len(elements) == 0 {
		return [][]byte{}
	}

	jsonElements := make([][]byte, 0, len(elements))
	for _, el := range elements {
		mapVal, err := mxj.NewMapXml(el, true)
		if err != nil {
			log.Println("could not parse XML chunk:", err)
			continue
		}
		jsonEl, err := mapVal.Json()
		if err != nil {
			log.Println("could not convert map to JSON:", err)
			continue
		}
		jsonElements = append(jsonElements, jsonEl)
	}

	return jsonElements
}

func (r *XmlFlattener) ConvertJSONToXML(jsonData []byte) ([]byte, error) {
	mapVal, err := mxj.NewMapJson(jsonData)
	if err != nil {
		return nil, err
	}

	xmlData, err := mapVal.Xml()
	if err != nil {
		return nil, err
	}
	return xmlData, nil
}
