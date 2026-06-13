package person

// jaroWinkler returns a similarity score in [0, 1] using the Jaro-Winkler
// formula with the standard prefix scale of 0.1 and a prefix cap of 4 chars.
// Implementation is the textbook one; see https://en.wikipedia.org/wiki/Jaro%E2%80%93Winkler_distance
func jaroWinkler(a, b string) float64 {
	if a == b {
		if a == "" {
			return 1.0
		}
		return 1.0
	}
	if a == "" || b == "" {
		return 0.0
	}

	ar := []rune(a)
	br := []rune(b)
	la, lb := len(ar), len(br)

	matchWindow := max(la, lb)/2 - 1
	if matchWindow < 0 {
		matchWindow = 0
	}

	matchA := make([]bool, la)
	matchB := make([]bool, lb)
	matches := 0
	for i := 0; i < la; i++ {
		start := i - matchWindow
		if start < 0 {
			start = 0
		}
		end := i + matchWindow + 1
		if end > lb {
			end = lb
		}
		for j := start; j < end; j++ {
			if matchB[j] {
				continue
			}
			if ar[i] != br[j] {
				continue
			}
			matchA[i] = true
			matchB[j] = true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0
	}

	transpositions := 0
	k := 0
	for i := 0; i < la; i++ {
		if !matchA[i] {
			continue
		}
		for !matchB[k] {
			k++
		}
		if ar[i] != br[k] {
			transpositions++
		}
		k++
	}
	transpositions /= 2

	m := float64(matches)
	jaro := (m/float64(la) + m/float64(lb) + (m-float64(transpositions))/m) / 3.0

	prefix := 0
	for i := 0; i < min(min(la, lb), 4); i++ {
		if ar[i] != br[i] {
			break
		}
		prefix++
	}
	return jaro + float64(prefix)*0.1*(1.0-jaro)
}

// jaccardTokens returns the Jaccard similarity between two token sets.
func jaccardTokens(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	set := make(map[string]int, len(a)+len(b))
	for _, t := range a {
		set[t] |= 1
	}
	for _, t := range b {
		set[t] |= 2
	}
	var inter, union int
	for _, v := range set {
		union++
		if v == 3 {
			inter++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
