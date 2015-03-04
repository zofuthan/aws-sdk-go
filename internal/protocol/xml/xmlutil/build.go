package xmlutil

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func BuildXML(params interface{}, e *xml.Encoder) error {
	b := xmlBuilder{encoder: e, namespaces: map[string]string{}}
	root := NewXMLElement(xml.Name{})
	if err := b.buildValue(reflect.ValueOf(params), root, ""); err != nil {
		return err
	}
	for _, c := range root.Children {
		for _, v := range c {
			return StructToXML(e, v, false)
		}
	}
	return nil
}

func elemOf(value reflect.Value) reflect.Value {
	for value.Kind() == reflect.Ptr {
		value = value.Elem()
	}
	return value
}

type xmlBuilder struct {
	encoder    *xml.Encoder
	namespaces map[string]string
}

func (b *xmlBuilder) buildValue(value reflect.Value, current *XMLNode, tag reflect.StructTag) error {
	value = elemOf(value)
	if !value.IsValid() { // no need to handle zero values
		return nil
	} else if tag.Get("location") != "" { // don't handle non-body location values
		return nil
	}

	t := tag.Get("type")
	if t == "" {
		switch value.Kind() {
		case reflect.Struct:
			t = "structure"
		case reflect.Slice:
			t = "list"
		case reflect.Map:
			t = "map"
		}
	}

	switch t {
	case "structure":
		if field, ok := value.Type().FieldByName("SDKShapeTraits"); ok {
			tag = tag + reflect.StructTag(" ") + field.Tag
		}
		return b.buildStruct(value, current, tag)
	case "list":
		return b.buildList(value, current, tag)
	case "map":
		return b.buildMap(value, current, tag)
	default:
		return b.buildScalar(value, current, tag)
	}
}

func (b *xmlBuilder) buildStruct(value reflect.Value, current *XMLNode, tag reflect.StructTag) error {
	if !value.IsValid() {
		return nil
	}

	fieldAdded := false

	// unwrap payloads
	if payload := tag.Get("payload"); payload != "" {
		field, _ := value.Type().FieldByName(payload)
		tag = field.Tag
		value = elemOf(value.FieldByName(payload))

		if !value.IsValid() {
			return nil
		}
	}

	child := NewXMLElement(xml.Name{Local: tag.Get("locationName")})

	// there is an xmlNamespace associated with this struct
	if prefix, uri := tag.Get("xmlPrefix"), tag.Get("xmlURI"); uri != "" {
		ns := xml.Attr{
			Name:  xml.Name{Local: "xmlns"},
			Value: uri,
		}
		if prefix != "" {
			b.namespaces[prefix] = uri // register the namespace
			ns.Name.Local = "xmlns:" + prefix
		}

		child.Attr = append(child.Attr, ns)
	}

	t := value.Type()
	for i := 0; i < value.NumField(); i++ {
		if c := t.Field(i).Name[0:1]; strings.ToLower(c) == c {
			continue // ignore unexported fields
		}

		member := elemOf(value.Field(i))
		field := t.Field(i)
		mTag := field.Tag

		if mTag.Get("location") != "" { // skip non-body members
			continue
		}

		memberName := mTag.Get("locationName")
		if memberName == "" {
			memberName = field.Name
			mTag = reflect.StructTag(string(mTag) + ` locationName:"` + memberName + `"`)
		}
		if err := b.buildValue(member, child, mTag); err != nil {
			return err
		}

		fieldAdded = true
	}

	if fieldAdded { // only append this child if we have one ore more valid members
		current.AddChild(child)
	}

	return nil
}

func (b *xmlBuilder) buildList(value reflect.Value, current *XMLNode, tag reflect.StructTag) error {
	// check for unflattened list member
	flattened := tag.Get("flattened") != ""

	xname := xml.Name{Local: tag.Get("locationName")}
	if flattened {
		for i := 0; i < value.Len(); i++ {
			child := NewXMLElement(xname)
			current.AddChild(child)
			if err := b.buildValue(value.Index(i), child, ""); err != nil {
				return err
			}
		}
	} else {
		list := NewXMLElement(xname)
		current.AddChild(list)

		for i := 0; i < value.Len(); i++ {
			iname := tag.Get("locationNameList")
			if iname == "" {
				iname = "member"
			}

			child := NewXMLElement(xml.Name{Local: iname})
			list.AddChild(child)
			if err := b.buildValue(value.Index(i), child, ""); err != nil {
				return err
			}
		}
	}

	return nil
}

func (b *xmlBuilder) buildMap(value reflect.Value, current *XMLNode, tag reflect.StructTag) error {
	// TODO(rest-xml-input-maps) implement support for REST-XML map inputs
	return fmt.Errorf("maps are not supported for this protocol")
}

func (b *xmlBuilder) buildScalar(value reflect.Value, current *XMLNode, tag reflect.StructTag) error {
	var str string
	switch converted := value.Interface().(type) {
	case string:
		str = converted
	case []byte:
		str = base64.StdEncoding.EncodeToString(converted)
	case bool:
		str = strconv.FormatBool(converted)
	case int64:
		str = strconv.FormatInt(converted, 10)
	case int:
		str = strconv.Itoa(converted)
	case float64:
		str = strconv.FormatFloat(converted, 'f', -1, 64)
	case float32:
		str = strconv.FormatFloat(float64(converted), 'f', -1, 32)
	case time.Time:
		const ISO8601UTC = "2006-01-02T15:04:05Z"
		str = converted.UTC().Format(ISO8601UTC)
	default:
		return fmt.Errorf("unsupported value for param %s: %v (%s)",
			tag.Get("locationName"), value.Interface(), value.Type().Name())
	}

	xname := xml.Name{Local: tag.Get("locationName")}
	if tag.Get("xmlAttribute") != "" { // put into current node's attribute list
		attr := xml.Attr{Name: xname, Value: str}
		current.Attr = append(current.Attr, attr)
	} else { // regular text node
		current.AddChild(&XMLNode{Name: xname, Text: str})
	}
	return nil
}