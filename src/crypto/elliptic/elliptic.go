// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package elliptic implements the standard NIST P-224, P-256, P-384, and P-521
// elliptic curves over prime fields.
package elliptic

import (
	"io"
	"math/big"
	"sync"
)

// A Curve represents a short-form Weierstrass curve with a=-3.
//
// The behavior of Add, Double, and ScalarMult when the input is not a point on
// the curve is undefined.
//
// Note that the conventional point at infinity (0, 0) is not considered on the
// curve, although it can be returned by Add, Double, ScalarMult, or
// ScalarBaseMult (but not the Unmarshal or UnmarshalCompressed functions).
type Curve interface {
	// Params returns the parameters for the curve.
	Params() *CurveParams
	// IsOnCurve reports whether the given (x,y) lies on the curve.
	IsOnCurve(x, y *big.Int) bool
	// Add returns the sum of (x1,y1) and (x2,y2)
	Add(x1, y1, x2, y2 *big.Int) (x, y *big.Int)
	// Double returns 2*(x,y)
	Double(x1, y1 *big.Int) (x, y *big.Int)
	// ScalarMult returns k*(Bx,By) where k is a number in big-endian form.
	ScalarMult(x1, y1 *big.Int, k []byte) (x, y *big.Int)
	// ScalarBaseMult returns k*G, where G is the base point of the group
	// and k is an integer in big-endian form.
	ScalarBaseMult(k []byte) (x, y *big.Int)
}

var mask = []byte{0xff, 0x1, 0x3, 0x7, 0xf, 0x1f, 0x3f, 0x7f}

// GenerateKey returns a public/private key pair. The private key is
// generated using the given reader, which must return random data.
func GenerateKey(curve Curve, rand io.Reader) (priv []byte, x, y *big.Int, err error) {
	N := curve.Params().N
	bitSize := N.BitLen()
	byteLen := (bitSize + 7) / 8
	priv = make([]byte, byteLen)

	for x == nil {
		_, err = io.ReadFull(rand, priv)
		if err != nil {
			return
		}
		// We have to mask off any excess bits in the case that the size of the
		// underlying field is not a whole number of bytes.
		priv[0] &= mask[bitSize%8]
		// This is because, in tests, rand will return all zeros and we don't
		// want to get the point at infinity and loop forever.
		priv[1] ^= 0x42

		// If the scalar is out of range, sample another random number.
		if new(big.Int).SetBytes(priv).Cmp(N) >= 0 {
			continue
		}

		x, y = curve.ScalarBaseMult(priv)
	}
	return
}

// Marshal converts a point on the curve into the uncompressed form specified in
// SEC 1, Version 2.0, Section 2.3.3. If the point is not on the curve (or is
// the conventional point at infinity), the behavior is undefined.
func Marshal(curve Curve, x, y *big.Int) []byte {
	byteLen := (curve.Params().BitSize + 7) / 8

	ret := make([]byte, 1+2*byteLen)
	ret[0] = 4 // uncompressed point

	x.FillBytes(ret[1 : 1+byteLen])
	y.FillBytes(ret[1+byteLen : 1+2*byteLen])

	return ret
}

// MarshalCompressed converts a point on the curve into the compressed form
// specified in SEC 1, Version 2.0, Section 2.3.3. If the point is not on the
// curve (or is the conventional point at infinity), the behavior is undefined.
func MarshalCompressed(curve Curve, x, y *big.Int) []byte {
	byteLen := (curve.Params().BitSize + 7) / 8
	compressed := make([]byte, 1+byteLen)
	compressed[0] = byte(y.Bit(0)) | 2
	x.FillBytes(compressed[1:])
	return compressed
}

// Unmarshal converts a point, serialized by Marshal, into an x, y pair. It is
// an error if the point is not in uncompressed form, is not on the curve, or is
// the point at infinity. On error, x = nil.
func Unmarshal(curve Curve, data []byte) (x, y *big.Int) {
	byteLen := (curve.Params().BitSize + 7) / 8
	if len(data) != 1+2*byteLen {
		return nil, nil
	}
	if data[0] != 4 { // uncompressed form
		return nil, nil
	}
	p := curve.Params().P
	x = new(big.Int).SetBytes(data[1 : 1+byteLen])
	y = new(big.Int).SetBytes(data[1+byteLen:])
	if x.Cmp(p) >= 0 || y.Cmp(p) >= 0 {
		return nil, nil
	}
	if !curve.IsOnCurve(x, y) {
		return nil, nil
	}
	return
}

// UnmarshalCompressed converts a point, serialized by MarshalCompressed, into
// an x, y pair. It is an error if the point is not in compressed form, is not
// on the curve, or is the point at infinity. On error, x = nil.
func UnmarshalCompressed(curve Curve, data []byte) (x, y *big.Int) {
	byteLen := (curve.Params().BitSize + 7) / 8
	if len(data) != 1+byteLen {
		return nil, nil
	}
	if data[0] != 2 && data[0] != 3 { // compressed form
		return nil, nil
	}
	p := curve.Params().P
	x = new(big.Int).SetBytes(data[1:])
	if x.Cmp(p) >= 0 {
		return nil, nil
	}
	// y² = x³ - 3x + b
	y = curve.Params().polynomial(x)
	y = y.ModSqrt(y, p)
	if y == nil {
		return nil, nil
	}
	if byte(y.Bit(0)) != data[0]&1 {
		y.Neg(y).Mod(y, p)
	}
	if !curve.IsOnCurve(x, y) {
		return nil, nil
	}
	return
}

var initonce sync.Once

func initAll() {
	initP224()
	initP256()
	initP384()
	initP521()
}

// P224 returns a Curve which implements NIST P-224 (FIPS 186-3, section D.2.2),
// also known as secp224r1. The CurveParams.Name of this Curve is "P-224".
//
// Multiple invocations of this function will return the same value, so it can
// be used for equality checks and switch statements.
//
// The cryptographic operations are implemented using constant-time algorithms.
func P224() Curve {
	initonce.Do(initAll)
	return p224
}

// P256 returns a Curve which implements NIST P-256 (FIPS 186-3, section D.2.3),
// also known as secp256r1 or prime256v1. The CurveParams.Name of this Curve is
// "P-256".
//
// Multiple invocations of this function will return the same value, so it can
// be used for equality checks and switch statements.
//
// ScalarMult and ScalarBaseMult are implemented using constant-time algorithms.
func P256() Curve {
	initonce.Do(initAll)
	return p256
}

// P384 returns a Curve which implements NIST P-384 (FIPS 186-3, section D.2.4),
// also known as secp384r1. The CurveParams.Name of this Curve is "P-384".
//
// Multiple invocations of this function will return the same value, so it can
// be used for equality checks and switch statements.
//
// The cryptographic operations are implemented using constant-time algorithms.
func P384() Curve {
	initonce.Do(initAll)
	return p384
}

// P521 returns a Curve which implements NIST P-521 (FIPS 186-3, section D.2.5),
// also known as secp521r1. The CurveParams.Name of this Curve is "P-521".
//
// Multiple invocations of this function will return the same value, so it can
// be used for equality checks and switch statements.
//
// The cryptographic operations are implemented using constant-time algorithms.
func P521() Curve {
	initonce.Do(initAll)
	return p521
}
