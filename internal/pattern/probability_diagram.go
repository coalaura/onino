package pattern

import "math/bits"

type probabilityBranch struct {
	low  uint32
	high uint32
	bit  uint16
}

type probabilityNode struct {
	branch probabilityBranch
	chance float64
}

type probabilityDiagram struct {
	nodes     []probabilityNode
	unique    map[probabilityBranch]uint32
	unions    map[[2]uint32]uint32
	limit     int
	remaining int
	complete  bool
}

func (diagram *probabilityDiagram) node(branch probabilityBranch) uint32 {
	if branch.low == branch.high {
		return branch.low
	}

	if index, exists := diagram.unique[branch]; exists {
		return index
	}

	if len(diagram.nodes) >= diagram.limit {
		diagram.complete = false

		return 0
	}

	index := uint32(len(diagram.nodes))
	chance := (diagram.nodes[branch.low].chance + diagram.nodes[branch.high].chance) * 0.5

	diagram.nodes = append(diagram.nodes, probabilityNode{branch: branch, chance: chance})
	diagram.unique[branch] = index

	return index
}

func (diagram *probabilityDiagram) condition(condition *probabilityCondition) uint32 {
	root := uint32(1)

	// Build from the last constrained bit, keeping every branch in bit order.
	for word := len(condition.mask) - 1; word >= 0; word-- {
		mask := condition.mask[word]

		for mask != 0 && diagram.complete {
			bit := bits.Len64(mask) - 1
			flag := uint64(1) << uint(bit)
			branch := probabilityBranch{bit: uint16(word*64 + bit)}

			if condition.value[word]&flag == 0 {
				branch.low = root
			} else {
				branch.high = root
			}

			root = diagram.node(branch)
			mask &^= flag
		}
	}

	return root
}

func (diagram *probabilityDiagram) union(left, right uint32) uint32 {
	if left == 1 || right == 1 {
		return 1
	}

	if left == 0 || left == right {
		return right
	}

	if right == 0 {
		return left
	}

	if left > right {
		left, right = right, left
	}

	pair := [2]uint32{left, right}
	if index, exists := diagram.unions[pair]; exists {
		return index
	}

	diagram.remaining--
	if !diagram.complete || diagram.remaining < 0 {
		diagram.complete = false

		return 0
	}

	leftBranch := diagram.nodes[left].branch
	rightBranch := diagram.nodes[right].branch

	bit := min(leftBranch.bit, rightBranch.bit)

	leftLow := left
	leftHigh := left
	rightLow := right
	rightHigh := right

	if leftBranch.bit == bit {
		leftLow = leftBranch.low
		leftHigh = leftBranch.high
	}

	if rightBranch.bit == bit {
		rightLow = rightBranch.low
		rightHigh = rightBranch.high
	}

	low := diagram.union(leftLow, rightLow)
	high := diagram.union(leftHigh, rightHigh)

	root := diagram.node(probabilityBranch{low: low, high: high, bit: bit})
	diagram.unions[pair] = root

	return root
}

func exactProbability(conditions []probabilityCondition, limit int) (float64, bool) {
	capacity := min(limit, 4096, max(2, len(conditions)*64+2))

	diagram := probabilityDiagram{
		nodes:     make([]probabilityNode, 2, capacity),
		unique:    make(map[probabilityBranch]uint32, capacity),
		unions:    make(map[[2]uint32]uint32, capacity),
		limit:     limit,
		remaining: limit * 16,
		complete:  true,
	}

	diagram.nodes[0].branch.bit = 260
	diagram.nodes[1].branch.bit = 260
	diagram.nodes[1].chance = 1

	root := uint32(0)

	for index := range conditions {
		condition := diagram.condition(&conditions[index])
		root = diagram.union(root, condition)

		if !diagram.complete {
			return 0, false
		}

		if root == 1 {
			return 1, true
		}
	}

	return diagram.nodes[root].chance, true
}
