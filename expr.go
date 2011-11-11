/*
Copyright (c) 2011 Andrew Wilkins <axwalk@gmail.com>

Permission is hereby granted, free of charge, to any person obtaining a copy of
this software and associated documentation files (the "Software"), to deal in
the Software without restriction, including without limitation the rights to
use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
of the Software, and to permit persons to whom the Software is furnished to do
so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package main

import (
    "fmt"
    "go/ast"
    "go/token"
    "reflect"
    "github.com/axw/gollvm/llvm"
)

func isglobal(value llvm.Value) bool {
    return !value.IsAGlobalVariable().IsNil()
}

func isindirect(value llvm.Value) bool {
    return !value.Metadata(llvm.MDKindID("indirect")).IsNil()
}

func setindirect(value llvm.Value) {
    value.SetMetadata(llvm.MDKindID("indirect"),
                      llvm.ConstNull(llvm.Int1Type()))
}

func (self *Visitor) VisitBinaryExpr(expr *ast.BinaryExpr) llvm.Value {
    x := self.VisitExpr(expr.X)
    y := self.VisitExpr(expr.Y)

    // If either is a const and the other is not, then cast the constant to the
    // other's type (to support untyped literals/expressions).
    x_const, y_const := x.IsConstant(), y.IsConstant()
    if x_const && !y_const {
        if isglobal(x) {x = x.Initializer()}
        if isindirect(y) {y = self.builder.CreateLoad(y, "")}
        x = self.maybeCast(x, y.Type())
    } else if !x_const && y_const {
        if isglobal(y) {y = y.Initializer()}
        if isindirect(x) {x = self.builder.CreateLoad(x, "")}
        y = self.maybeCast(y, x.Type())
    } else if x_const && y_const {
        // If either constant is a global variable, 'dereference' it by taking
        // its initializer, which will never change.
        if isglobal(x) {x = x.Initializer()}
        if isglobal(y) {y = y.Initializer()}
    }

    // TODO check types/sign, use float operators if appropriate.
    switch expr.Op {
    case token.MUL:
        if x_const && y_const {
            return llvm.ConstMul(x, y)
        } else {
            return self.builder.CreateMul(x, y, "")
        }
    case token.QUO:
        if x_const && y_const {
            return llvm.ConstUDiv(x, y)
        } else {
            return self.builder.CreateUDiv(x, y, "")
        }
    case token.ADD:
        if x_const && y_const {
            return llvm.ConstAdd(x, y)
        } else {
            return self.builder.CreateAdd(x, y, "")
        }
    case token.SUB:
        if x_const && y_const {
            return llvm.ConstSub(x, y)
        } else {
            return self.builder.CreateSub(x, y, "")
        }
    case token.EQL:
        if x_const && y_const {
            return llvm.ConstICmp(llvm.IntEQ, x, y)
        } else {
            return self.builder.CreateICmp(llvm.IntEQ, x, y, "")
        }
    case token.LSS:
        if x_const && y_const {
            return llvm.ConstICmp(llvm.IntULT, x, y)
        } else {
            return self.builder.CreateICmp(llvm.IntULT, x, y, "")
        }
    }
    panic(fmt.Sprint("Unhandled operator: ", expr.Op))
}

func (self *Visitor) VisitUnaryExpr(expr *ast.UnaryExpr) llvm.Value {
    value := self.VisitExpr(expr.X)
    switch expr.Op {
    case token.SUB: {
        if !value.IsAConstant().IsNil() {
            value = llvm.ConstNeg(value)
        } else {
            value = self.builder.CreateNeg(value, "")
        }
    }
    case token.ADD: {/*No-op*/}
    default: panic("Unhandled operator: ")// + expr.Op)
    }
    return value
}

func (self *Visitor) VisitCallExpr(expr *ast.CallExpr) llvm.Value {
    switch x := (expr.Fun).(type) {
    case *ast.Ident: {
        switch x.String() {
        case "println": {return self.VisitPrintln(expr)}
        case "len": {return self.VisitLen(expr)}
        default: {
            // Is it a type? Then this is a conversion (e.g. int(123))
            if expr.Args != nil && len(expr.Args) == 1 {
                typ := self.GetType(x)
                if !typ.IsNil() {
                    value := self.VisitExpr(expr.Args[0])
                    return self.maybeCast(value, typ)
                }
            }

            fn, obj := self.Resolve(x.Obj), x.Obj
            if fn.IsNil() {
                panic(fmt.Sprintf(
                    "No function found with name '%s'", x.String()))
            } else if obj.Kind == ast.Var {
                fn = self.builder.CreateLoad(fn, "")
            }

            // TODO handle varargs
            var args []llvm.Value = nil
            if expr.Args != nil {
                args = make([]llvm.Value, len(expr.Args))
                for i, expr := range expr.Args {args[i] = self.VisitExpr(expr)}
            }
            return self.builder.CreateCall(fn, args, "")
        }
        }
    }
    }
    panic("Unhandled CallExpr")
}

func (self *Visitor) VisitIndexExpr(expr *ast.IndexExpr) llvm.Value {
    value := self.VisitExpr(expr.X)
    // TODO handle maps, strings, slices.

    index := self.VisitExpr(expr.Index)
    if isindirect(index) {index = self.builder.CreateLoad(index, "")}
    if index.Type().TypeKind() != llvm.IntegerTypeKind {
        panic("Array index expression must evaluate to an integer")
    }

    // Is it an array? Then let's get the address of the array so we can
    // get an element.
    if value.Type().TypeKind() == llvm.ArrayTypeKind {
        value = value.Metadata(llvm.MDKindID("address"))
    }

    zero := llvm.ConstInt(llvm.Int32Type(), 0, false)
    element := self.builder.CreateGEP(value, []llvm.Value{zero, index}, "")
    return self.builder.CreateLoad(element, "")
}

func (self *Visitor) VisitExpr(expr ast.Expr) llvm.Value {
    switch x:= expr.(type) {
    case *ast.BasicLit: return self.VisitBasicLit(x)
    case *ast.BinaryExpr: return self.VisitBinaryExpr(x)
    case *ast.FuncLit: return self.VisitFuncLit(x)
    case *ast.CompositeLit: return self.VisitCompositeLit(x)
    case *ast.UnaryExpr: return self.VisitUnaryExpr(x)
    case *ast.CallExpr: return self.VisitCallExpr(x)
    case *ast.IndexExpr: return self.VisitIndexExpr(x)
    case *ast.Ident: {
        if x.Obj == nil {x.Obj = self.LookupObj(x.Name)}
        return self.Resolve(x.Obj)
    }
    }
    panic(fmt.Sprintf("Unhandled Expr node: %s", reflect.TypeOf(expr)))
}

// vim: set ft=go :
