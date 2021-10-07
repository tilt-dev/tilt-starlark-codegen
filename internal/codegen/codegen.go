package codegen

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iancoleman/strcase"
	"k8s.io/gengo/parser"
	"k8s.io/gengo/types"
)

// Find all top-level types with the tilt:starlark-gen=true tag.
func LoadStarlarkGenTypes(pkg string) (*types.Package, []*types.Type, error) {
	b := parser.New()
	u := types.Universe{}
	pkgSpec, err := b.AddDirectoryTo(pkg, &u)
	if err != nil {
		return nil, nil, err
	}

	results := []*types.Type{}
	for _, t := range pkgSpec.Types {
		ok, err := types.ExtractSingleBoolCommentTag("+", "tilt:starlark-gen", false, t.CommentLines)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing tags in %s: %v", t, err)
		}
		if ok {
			results = append(results, t)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name.Name < results[j].Name.Name
	})

	return pkgSpec, results, nil
}

func getSpecMemberType(t *types.Type) *types.Type {
	for _, member := range t.Members {
		if member.Name == "Spec" {
			return member.Type
		}
	}
	return nil
}

// Special-case config map, which only has one member: Data
func getDataMember(t *types.Type) *types.Member {
	for _, member := range t.Members {
		if member.Name == "Data" {
			return &member
		}
	}
	return nil
}

// Opens the output file.
func OpenOutputFile(outDir string) (w io.Writer, path string, err error) {
	if outDir == "-" {
		return os.Stdout, "stdout", nil
	}

	outPath := filepath.Join(outDir, "types.go")

	out, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0555)
	if err != nil {
		return nil, outPath, err
	}
	return out, outPath, nil
}

// Writes the package header.
func WritePreamble(pkg *types.Package, w io.Writer) error {

	_, err := fmt.Fprintf(w, `package %s

import (
	"go.starlark.net/starlark"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/tilt-dev/tilt/internal/tiltfile/starkit"
	"github.com/tilt-dev/tilt/internal/tiltfile/value"
	"github.com/tilt-dev/tilt/pkg/apis/core/v1alpha1"
)

// AUTOGENERATED by github.com/tilt-dev/tilt-starlark-codegen
// DO NOT EDIT MANUALLY
`, pkg.Name)
	if err != nil {
		return err
	}
	return nil
}

// Writes a function that registers all the starlark methods.
func WriteStarlarkRegistrationFunc(types []*types.Type, pkg *types.Package, w io.Writer) error {
	_, err := fmt.Fprintf(w, `
func (p Plugin) registerSymbols(env *starkit.Environment) error {
  var err error
`)
	if err != nil {
		return err
	}

	for _, t := range types {
		tName := t.Name.Name
		_, err := fmt.Fprintf(w, `
	err = env.AddBuiltin("%s.%s", p.%s)
	if err != nil {
		return err
	}`, pkg.Name, strcase.ToSnake(tName), strcase.ToLowerCamel(tName))
		if err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(w, `
  return nil
}`)
	return err
}

func unpackMemberVarName(m types.Member) string {
	if m.Name == "Args" {
		return "specArgs"
	}
	if m.Name == "Labels" {
		return "specLabels"
	}
	if m.Name == "Annotations" {
		return "specAnnotations"
	}
	return strcase.ToLowerCamel(m.Name)
}

type memberVar struct {
	Type    string
	Name    string
	Initial string
}

// Helper function to determine how to unpack fields.
func unpackMemberVar(m types.Member) (memberVar, error) {
	isLocalPath, err := types.ExtractSingleBoolCommentTag("+", "tilt:local-path", false, m.CommentLines)
	if err != nil {
		return memberVar{}, fmt.Errorf("parsing tags in %s: %v", m.Name, err)
	}

	mName := unpackMemberVarName(m)
	t := m.Type
	if t.Kind == types.Builtin {
		if isLocalPath {
			return memberVar{
				Type: "value.LocalPath",
				Name: mName,
				Initial: fmt.Sprintf(`= value.NewLocalPathUnpacker(t)
  err = %s.Unpack(starlark.String(""))
  if err != nil {
    return nil, err
  }
`, mName),
			}, nil
		}

		return memberVar{Name: fmt.Sprintf("obj.Spec.%s", m.Name)}, nil
	}

	if t.Kind == types.Struct {
		return memberVar{
			Type:    t.Name.Name,
			Name:    unpackMemberVarName(m),
			Initial: fmt.Sprintf("= %s{t: t}", t.Name.Name),
		}, nil
	}

	if t.Kind == types.Pointer {
		if t.Elem.Kind == types.Struct {
			return memberVar{
				Type:    t.Elem.Name.Name,
				Name:    unpackMemberVarName(m),
				Initial: fmt.Sprintf("= %s{t: t}", t.Elem.Name.Name),
			}, nil
		}
	}

	if t.Kind == types.Map {
		return memberVar{
			Type: "value.StringStringMap",
			Name: unpackMemberVarName(m),
		}, nil
	}

	if t.Kind == types.Slice {
		if t.Elem.Kind == types.Builtin && t.Elem.Name.Name == "string" {
			if isLocalPath {
				return memberVar{
					Type:    "value.LocalPathList",
					Name:    unpackMemberVarName(m),
					Initial: "= value.NewLocalPathListUnpacker(t)",
				}, nil
			} else {
				return memberVar{
					Type: "value.StringList",
					Name: unpackMemberVarName(m),
				}, nil
			}
		}

		if t.Elem.Kind == types.Struct {
			return memberVar{
				Type:    fmt.Sprintf("%sList", t.Elem.Name.Name),
				Name:    unpackMemberVarName(m),
				Initial: fmt.Sprintf("= %sList{t: t}", t.Elem.Name.Name),
			}, nil
		}
	}
	return memberVar{}, fmt.Errorf("Cannot unpack member %s", m.Name)
}

func modelTypeName(t *types.Type) string {
	if strings.Contains(t.Name.Package, "/meta") {
		return fmt.Sprintf("metav1.%s", t.Name.Name)
	}

	parts := strings.Split(t.Name.Package, "/")
	return fmt.Sprintf("%s.%s", parts[len(parts)-1], t.Name.Name)
}

// Given a gengo Type, create a starlark function that reads that type.
func WriteStarlarkAPIObjectFunction(t *types.Type, pkg *types.Package, w io.Writer) error {
	tName := t.Name.Name
	fnName := strcase.ToLowerCamel(tName)
	spec := getSpecMemberType(t)
	data := getDataMember(t)
	var members []types.Member
	if spec != nil {
		members = spec.Members
	} else if data != nil {
		members = []types.Member{*data}
	} else {
		return fmt.Errorf("type has no spec or data field: %s", tName)
	}

	objTypeName := modelTypeName(t)

	// Print the function signature.
	_, err := fmt.Fprintf(w, `
func (p Plugin) %s(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {`,
		fnName)
	if err != nil {
		return err
	}

	// Print the object initializer.
	specInit := ""
	if spec != nil {
		specInit = fmt.Sprintf(`
    Spec: %sSpec{},`, objTypeName)
	}
	_, err = fmt.Fprintf(w, `
  var err error
	obj := &%s{
		ObjectMeta: metav1.ObjectMeta{},%s
	}`, objTypeName, specInit)
	if err != nil {
		return err
	}

	// Print any special unpack vars.
	for _, member := range members {
		memberVar, err := unpackMemberVar(member)
		if err != nil {
			return fmt.Errorf("generating type %s: %v", tName, err)
		}

		if memberVar.Type == "" {
			continue
		}

		_, err = fmt.Fprintf(w, `
  var %s %s %s`, memberVar.Name, memberVar.Type, memberVar.Initial)
		if err != nil {
			return err
		}
	}

	// Print the object unpacker.
	_, err = fmt.Fprintf(w, `
  var labels value.StringStringMap
	var annotations value.StringStringMap
	err = starkit.UnpackArgs(t, fn.Name(), args, kwargs,
		"name", &obj.ObjectMeta.Name,
		"labels?", &labels,
		"annotations?", &annotations,`)
	if err != nil {
		return err
	}

	// Print unpackers of individual members.
	for _, member := range members {
		memberVar, _ := unpackMemberVar(member)
		_, err = fmt.Fprintf(w, `
    "%s?", &%s,`, strcase.ToSnake(member.Name), memberVar.Name)
		if err != nil {
			return err
		}
	}

	// Print the end of arg parsing.
	_, err = fmt.Fprintf(w, `
  )
  if err != nil {
    return nil, err
  }
`)
	if err != nil {
		return err
	}

	// Copy unpackers into the object.
	for _, member := range members {
		memberVar, _ := unpackMemberVar(member)
		unpackOptional := false
		if memberVar.Type == "" {
			continue
		}
		varN := memberVar.Name
		if memberVar.Initial != "" {
			varN = varN + ".Value"
		}
		if member.Type.Kind == types.Pointer {
			varN = "&" + varN
			if member.Type.Elem.Kind == types.Struct {
				varN = fmt.Sprintf("(*%s)(%s)", modelTypeName(member.Type.Elem), varN)
				unpackOptional = true
			}
		}

		if member.Type.Kind == types.Struct {
			varN = fmt.Sprintf("%s(%s)", modelTypeName(member.Type), varN)
		}

		if unpackOptional {
			_, err = fmt.Fprintf(w, `
    if %s.isUnpacked {`, unpackMemberVarName(member))
			if err != nil {
				return err
			}
		}

		if spec != nil {
			_, err = fmt.Fprintf(w, `
    obj.Spec.%s = %s`, member.Name, varN)
			if err != nil {
				return err
			}
		} else {
			_, err = fmt.Fprintf(w, `
    obj.%s = %s`, member.Name, varN)
			if err != nil {
				return err
			}
		}

		if unpackOptional {
			_, err = fmt.Fprintf(w, `
    }`)
			if err != nil {
				return err
			}
		}
	}

	// Register the type.
	_, err = fmt.Fprintf(w, `
  obj.ObjectMeta.Labels = labels
  obj.ObjectMeta.Annotations = annotations
	return p.register(t, obj)
}
`)
	if err != nil {
		return err
	}
	return nil
}

// Given a member list struct type, we need to 2 pieces:
// 1) A starlark type so that this struct can be passed around.
// 2) An Unpack() function so that this struct can be read from a list.
func WriteStarlarkStructListFunction(t *types.Type, pkg *types.Package, w io.Writer) error {

	tName := t.Name.Name

	// Embed a frozen list so this can be accessed.
	_, err := fmt.Fprintf(w, `
type %sList struct {
  *starlark.List
  Value []%s
  t *starlark.Thread
}

func (o *%sList) Unpack(v starlark.Value) error {
	items := []%s{}

  listObj, ok := v.(*starlark.List)
  if !ok {
    return fmt.Errorf("expected list, actual: %%v", v.Type())
  }

  for i := 0; i < listObj.Len(); i++ {
    v := listObj.Index(i)

    item := %s{t: o.t}
    err := item.Unpack(v)
    if err != nil {
      return fmt.Errorf("at index %%d: %%v", i, err)
    }
    items = append(items, %s(item.Value))
  }

  listObj.Freeze()
  o.List = listObj
  o.Value = items

  return nil
}`, tName, modelTypeName(t), tName, modelTypeName(t), tName, modelTypeName(t))
	if err != nil {
		return err
	}
	return nil
}

// Given a member struct type, we need to 3 pieces:
// 1) A starlark type so that this struct can be passed around.
// 2) An Unpack() function so that this struct can be read from a dict.
// 3) A built-in function that constructs the object natively.
func WriteStarlarkStructFunction(t *types.Type, pkg *types.Package, w io.Writer) error {

	tName := t.Name.Name

	// The built-in struct is composed of
	// 1) An embedded, frozen Dict
	// 2) A first-class representation of the API type.
	_, err := fmt.Fprintf(w, `
type %s struct {
  *starlark.Dict
  Value %s
  isUnpacked bool
  t *starlark.Thread // instantiation thread for computing abspath
}
`, tName, modelTypeName(t))
	if err != nil {
		return err
	}

	err = writeStarlarkStructConstructor(t, pkg, w)
	if err != nil {
		return err
	}

	return writeStarlarkStructUnpacker(t, pkg, w)
}

// For each struct in the API that's not a top-level type, create
// a built-in function that returns the struct and lets you pass it around.
func writeStarlarkStructConstructor(t *types.Type, pkg *types.Package, w io.Writer) error {
	tName := t.Name.Name
	fnName := strcase.ToLowerCamel(tName)

	// Print the function signature.
	_, err := fmt.Fprintf(w, `
func (p Plugin) %s(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {`,
		fnName)
	if err != nil {
		return err
	}

	// Unpack each argument into a starlark.Value
	for _, m := range t.Members {
		_, err = fmt.Fprintf(w, `
  var %s starlark.Value`, unpackMemberVarName(m))
		if err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(w, `
	err := starkit.UnpackArgs(t, fn.Name(), args, kwargs,`)
	if err != nil {
		return err
	}

	for _, member := range t.Members {
		_, err = fmt.Fprintf(w, `
    "%s?", &%s,`, strcase.ToSnake(member.Name), unpackMemberVarName(member))
		if err != nil {
			return err
		}
	}

	// Print the end of arg parsing.
	_, err = fmt.Fprintf(w, `
  )
  if err != nil {
    return nil, err
  }
`)
	if err != nil {
		return err
	}

	// Create a dict from the args, then use dict-based unpacking.
	_, err = fmt.Fprintf(w, `
  dict := starlark.NewDict(%d)
`, len(t.Members))
	if err != nil {
		return err
	}

	for _, member := range t.Members {
		mName := unpackMemberVarName(member)
		_, err = fmt.Fprintf(w, `
  if %s != nil {
    err := dict.SetKey(starlark.String("%s"), %s)
    if err != nil {
      return nil, err
    }
  }`, mName,
			strcase.ToSnake(member.Name),
			mName)
		if err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(w, `
  var obj *%s = &%s{t: t}
  err = obj.Unpack(dict)
  if err != nil {
    return nil, err
  }
  return obj, nil
}
`, tName, tName)
	if err != nil {
		return err
	}

	return nil
}

func writeStarlarkStructUnpacker(t *types.Type, pkg *types.Package, w io.Writer) error {
	tName := t.Name.Name

	// Print the Unpack() signature.
	_, err := fmt.Fprintf(w, `
func (o *%s) Unpack(v starlark.Value) error {`,
		tName)
	if err != nil {
		return err
	}

	// Zero out the object, and start iterating over the value.
	_, err = fmt.Fprintf(w, `
	obj := %s{}

  starlarkObj, ok := v.(*%s)
  if ok {
    *o = *starlarkObj
    return nil
  }

  mapObj, ok := v.(*starlark.Dict)
  if !ok {
    return fmt.Errorf("expected dict, actual: %%v", v.Type())
  }

  for _, item := range mapObj.Items() {
    keyV, val := item[0], item[1]
    key, ok := starlark.AsString(keyV)
    if !ok {
      return fmt.Errorf("key must be string. Got: %%s", keyV.Type())
    }
`, modelTypeName(t), tName)
	if err != nil {
		return err
	}

	// Unpack each attribute.
	for _, m := range t.Members {
		err := writeAttrUnpacker(m, pkg, w)
		if err != nil {
			return fmt.Errorf("generating %s unpacker: %v", t.Name.Name, err)
		}
	}

	_, err = fmt.Fprintf(w, `
    return fmt.Errorf("Unexpected attribute name: %%s", key)
  }

  mapObj.Freeze()
  o.Dict = mapObj
  o.Value = obj
  o.isUnpacked = true

  return nil
}`)
	if err != nil {
		return err
	}
	return nil
}

func isTimeMember(m types.Member) bool {
	if m.Type.Kind == types.Pointer && m.Type.Elem.Kind == types.Struct {
		elName := m.Type.Elem.Name.Name
		if elName == "Time" || elName == "MicroTime" {
			return true
		}
	}
	if m.Type.Kind == types.Struct {
		tName := m.Type.Name.Name
		if tName == "Time" || tName == "MicroTime" {
			return true
		}
	}
	return false
}

// Recursive helper function for unpacking individual members
// of a struct.
func writeAttrUnpacker(m types.Member, pkg *types.Package, w io.Writer) error {
	if m.Embedded {
		for _, embeddedMember := range m.Type.Members {
			err := writeAttrUnpacker(embeddedMember, pkg, w)
			if err != nil {
				return err
			}
		}
		return nil
	}

	// Skip Time and MicroTime for now.
	if isTimeMember(m) {
		return nil
	}

	_, err := fmt.Fprintf(w, `
    if key == "%s" {`, strcase.ToSnake(m.Name))
	if err != nil {
		return err
	}

	isLocalPath, err := types.ExtractSingleBoolCommentTag("+", "tilt:local-path", false, m.CommentLines)
	if err != nil {
		return fmt.Errorf("parsing tags in %s: %v", m.Name, err)
	}

	if m.Type.Kind == types.Builtin ||
		(m.Type.Kind == types.Alias && m.Type.Underlying.Kind == types.Builtin) ||
		(m.Type.Kind == types.Pointer && m.Type.Elem.Kind == types.Builtin) {
		cast := fmt.Sprintf("%s(v)", m.Type.Name.Name)
		isInt := m.Type.Name.Name == "int32"
		isBool := m.Type.Name.Name == "bool"
		if m.Type.Kind == types.Alias {
			cast = fmt.Sprintf("%s(v)", modelTypeName(m.Type))
			isInt = m.Type.Underlying.Name.Name == "int32"
		} else if m.Type.Kind == types.Pointer {
			cast = fmt.Sprintf("(*%s)(&v)", m.Type.Elem.Name.Name)
			isInt = m.Type.Elem.Name.Name == "int32"
		}

		if isBool {
			// Unpack bools
			_, err = fmt.Fprintf(w, `
      v, ok := val.(starlark.Bool)
      if !ok {
        return fmt.Errorf("Expected bool, got: %%v", val.Type())
      }
      obj.%s = bool(v)
      continue`, m.Name)
			if err != nil {
				return err
			}
		} else if isInt {
			// Unpack ints
			_, err = fmt.Fprintf(w, `
      v, err := starlark.AsInt32(val)
      if err != nil {
        return fmt.Errorf("Expected int, got: %%v", err)
      }
      obj.%s = %s
      continue`, m.Name, cast)
			if err != nil {
				return err
			}
		} else if isLocalPath {
			// Unpack local path lists
			_, err = fmt.Fprintf(w, `
      v := value.NewLocalPathUnpacker(o.t)
      err := v.Unpack(val)
      if err != nil {
        return fmt.Errorf("unpacking %%s: %%v", key, err)
      }
      obj.%s = v.Value
      continue`, m.Name)
			if err != nil {
				return err
			}
		} else {
			// Unpack strings
			_, err = fmt.Fprintf(w, `
      v, ok := starlark.AsString(val)
      if !ok {
        return fmt.Errorf("Expected string, actual: %%s", val.Type())
      }
      obj.%s = %s
      continue`, m.Name, cast)
			if err != nil {
				return err
			}
		}
	} else if m.Type.Kind == types.Slice &&
		m.Type.Elem.Kind == types.Builtin && m.Type.Elem.Name.Name == "string" {
		if isLocalPath {
			_, err = fmt.Fprintf(w, `
      v := value.NewLocalPathListUnpacker(o.t)
      err := v.Unpack(val)
      if err != nil {
        return fmt.Errorf("unpacking %%s: %%v", key, err)
      }
      obj.%s = v
      continue`, m.Name)
			if err != nil {
				return err
			}
		} else {
			_, err = fmt.Fprintf(w, `
      var v value.StringList
      err := v.Unpack(val)
      if err != nil {
        return fmt.Errorf("unpacking %%s: %%v", key, err)
      }
      obj.%s = v
      continue`, m.Name)
			if err != nil {
				return err
			}
		}
	} else if m.Type.Kind == types.Slice &&
		m.Type.Elem.Kind == types.Struct {
		_, err = fmt.Fprintf(w, `
      v := %sList{t: o.t}
      err := v.Unpack(val)
      if err != nil {
        return fmt.Errorf("unpacking %%s: %%v", key, err)
      }
      obj.%s = v.Value
      continue`, m.Type.Elem.Name.Name, m.Name)
		if err != nil {
			return err
		}
	} else if m.Type.Kind == types.Struct {
		_, err = fmt.Fprintf(w, `
      v := %s{t: o.t}
      err := v.Unpack(val)
      if err != nil {
        return fmt.Errorf("unpacking %%s: %%v", key, err)
      }
      obj.%s = v.Value
      continue`, m.Type.Name.Name, m.Name)
		if err != nil {
			return err
		}
	} else if m.Type.Kind == types.Pointer && m.Type.Elem.Kind == types.Struct {
		_, err = fmt.Fprintf(w, `
      v := %s{t: o.t}
      err := v.Unpack(val)
      if err != nil {
        return fmt.Errorf("unpacking %%s: %%v", key, err)
      }
      obj.%s = (*%s)(&v.Value)
      continue`, m.Type.Elem.Name.Name, m.Name, modelTypeName(m.Type.Elem))
		if err != nil {
			return err
		}
	} else if m.Type.Kind == types.Map && m.Type.Elem.Name.Name == "string" && m.Type.Key.Name.Name == "string" {
		_, err = fmt.Fprintf(w, `
      var v value.StringStringMap
      err := v.Unpack(val)
      if err != nil {
        return fmt.Errorf("unpacking %%s: %%v", key, err)
      }
      obj.%s = (map[string]string)(v)
      continue`, m.Name)
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("Unable to unpack attribute %s type %s", m.Name, m.Type)
	}

	_, err = fmt.Fprintf(w, `
    }`)
	if err != nil {
		return err
	}
	return nil
}
