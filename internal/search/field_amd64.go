//go:build !purego

package search

var fastFieldAvailable = supportsBMI2ADX()

//go:noescape
func supportsBMI2ADX() bool

//go:noescape
//go:abiinternal result=AX left=BX right=CX ->
func multiplyBMI2(result, left, right *fieldElement)
