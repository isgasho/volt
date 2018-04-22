package dsl

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/vim-volt/volt/dsl/dslctx"
	"github.com/vim-volt/volt/dsl/types"
	"github.com/vim-volt/volt/pathutil"
	"github.com/vim-volt/volt/transaction"
)

// Execute executes given expr with given ctx.
func Execute(ctx context.Context, expr types.Expr) (_ types.Value, result error) {
	if err := dslctx.Validate(ctx); err != nil {
		return nil, err
	}

	// Begin transaction
	trx, err := transaction.Start()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := trx.Done(); err != nil {
			result = err
		}
	}()

	// Expand all macros before write
	expr, err = expandMacro(expr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to expand macros")
	}

	// Write given expression to $VOLTPATH/trx/lock/log.json
	err = writeExpr(expr)
	if err != nil {
		return nil, err
	}

	val, rollback, err := expr.Eval(ctx)
	if err != nil {
		rollback()
		return nil, errors.Wrap(err, "expression returned an error")
	}
	return val, nil
}

func expandMacro(expr types.Expr) (types.Expr, error) {
	val, err := doExpandMacro(expr)
	if err != nil {
		return nil, err
	}
	result, ok := val.(types.Expr)
	if !ok {
		return nil, errors.New("the result of expansion of macros must be an expression")
	}
	return result, nil
}

func writeExpr(expr types.Expr) error {
	deparsed, err := Deparse(expr)
	if err != nil {
		return errors.Wrap(err, "failed to deparse expression")
	}

	filename := filepath.Join(pathutil.TrxDir(), "lock", "log.json")
	logFile, err := os.Create(filename)
	if err != nil {
		return errors.Wrapf(err, "could not create %s", filename)
	}
	_, err = io.Copy(logFile, bytes.NewReader(deparsed))
	if err != nil {
		return errors.Wrapf(err, "failed to write transaction log %s", filename)
	}
	err = logFile.Close()
	if err != nil {
		return errors.Wrapf(err, "failed to close transaction log %s", filename)
	}
	return nil
}

// doExpandMacro expands macro's expression recursively
func doExpandMacro(expr types.Expr) (types.Value, error) {
	op := expr.Op()
	if !op.IsMacro() {
		return expr, nil
	}
	args := expr.Args()
	for i := range args {
		if inner, ok := args[i].(types.Expr); ok {
			v, err := doExpandMacro(inner)
			if err != nil {
				return nil, err
			}
			args[i] = v
		}
	}
	// XXX: should we care rollback function?
	val, _, err := op.EvalExpr(context.Background(), args)
	return val, err
}
