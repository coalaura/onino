//go:build !purego

package search

//go:noescape
//go:abiinternal result=AX left=BX right=CX ->
func multiplyBMI2Only(result, left, right *fieldElement)

//go:noescape
//go:abiinternal result=AX source=BX ->
func squareBMI2Only(result, source *fieldElement)

//go:noescape
//go:abiinternal point=AX scratch=BX step=CX ->
func advanceBMI2Only(point *extendedPoint, scratch *[8]fieldElement, step *affineStep)

//go:noescape
//go:abiinternal point=AX product=BX publicKey=CX count=DI reciprocal=SI ->
func normalizeBMI2Only(point *extendedPoint, product *fieldElement, publicKey *[32]byte, count int, reciprocal *fieldElement)

//go:noescape
//go:abiinternal center=AX scratch=BX offset=CX ->
func pairedPrepareBMI2Only(center *pairedAffine, scratch *pairedScratch, offset *pairedAffine)

//go:noescape
//go:abiinternal scratch=AX inverse=BX ->
func pairedInverseBMI2Only(scratch *pairedScratch, inverse *fieldElement)
