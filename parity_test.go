package pixiu_router_update

import (
	newrouter "github.com/alanxtl/pixiu-router-update/new"
	newmodel "github.com/alanxtl/pixiu-router-update/new/model"
	oldrouter "github.com/alanxtl/pixiu-router-update/old"
	"math/rand"
	"net/http"
	"strconv"
	"testing"

	oldmodel "github.com/alanxtl/pixiu-router-update/old/model"
)

/* ==============================
   build tools
   ============================== */

type HeaderSpec struct {
	Name   string
	Values []string
	Regex  bool
}

type RouteSpec struct {
	ID      string
	Methods []string
	Path    string
	Prefix  string
	Headers []HeaderSpec
	Cluster string
}

func (s RouteSpec) toOld() *oldmodel.Router {
	h := make([]oldmodel.HeaderMatcher, 0, len(s.Headers))
	for _, x := range s.Headers {
		h = append(h, oldmodel.HeaderMatcher{Name: x.Name, Values: append([]string(nil), x.Values...), Regex: x.Regex})
	}
	return &oldmodel.Router{
		ID: s.ID,
		Match: oldmodel.RouterMatch{
			Methods: append([]string(nil), s.Methods...),
			Path:    s.Path,
			Prefix:  s.Prefix,
			Headers: h,
		},
		Route: oldmodel.RouteAction{Cluster: s.Cluster},
	}
}

func (s RouteSpec) toNew() *newmodel.Router {
	h := make([]newmodel.HeaderMatcher, 0, len(s.Headers))
	for _, x := range s.Headers {
		h = append(h, newmodel.HeaderMatcher{Name: x.Name, Values: append([]string(nil), x.Values...), Regex: x.Regex})
	}
	return &newmodel.Router{
		ID: s.ID,
		Match: newmodel.RouterMatch{
			Methods: append([]string(nil), s.Methods...),
			Path:    s.Path,
			Prefix:  s.Prefix,
			Headers: h,
		},
		Route: newmodel.RouteAction{Cluster: s.Cluster},
	}
}

func buildOld(specs []RouteSpec) *oldrouter.RouterCoordinator {
	rs := make([]*oldmodel.Router, 0, len(specs))
	for _, s := range specs {
		rs = append(rs, s.toOld())
	}
	cfg := &oldmodel.RouteConfiguration{Routes: rs, Dynamic: false}
	return oldrouter.CreateRouterCoordinator(cfg)
}

func buildNew(specs []RouteSpec) *newrouter.RouterCoordinator {
	rs := make([]*newmodel.Router, 0, len(specs))
	for _, s := range specs {
		rs = append(rs, s.toNew())
	}
	cfg := &newmodel.RouteConfiguration{Routes: rs, Dynamic: false}
	return newrouter.CreateRouterCoordinator(cfg)
}

type res struct {
	ok      bool
	cluster string
	err     string
}

func call(cOld *oldrouter.RouterCoordinator, cNew *newrouter.RouterCoordinator, method, path string, hdr map[string]string) (res, res) {
	req, _ := http.NewRequest(method, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	oa, oe := cOld.Route(req)
	na, ne := cNew.Route(req)

	or := res{}
	if oe != nil || oa == nil {
		if oe != nil {
			or.err = oe.Error()
		}
	} else {
		or.ok = true
		or.cluster = oa.Cluster
	}

	nr := res{}
	if ne != nil || na == nil {
		if ne != nil {
			nr.err = ne.Error()
		}
	} else {
		nr.ok = true
		nr.cluster = na.Cluster
	}
	return or, nr
}

func assertSame(t *testing.T, oldc *oldrouter.RouterCoordinator, newc *newrouter.RouterCoordinator,
	method, path string, hdr map[string]string, wantOK bool, wantCluster string) {

	ro, rn := call(oldc, newc, method, path, hdr)
	if ro.ok != rn.ok || ro.cluster != rn.cluster {
		t.Fatalf("mismatch: %s %s hdr=%v\n old={ok:%v cluster:%q err:%q}\n new={ok:%v cluster:%q err:%q}",
			method, path, hdr, ro.ok, ro.cluster, ro.err, rn.ok, rn.cluster, rn.err)
	}
	if ro.ok != wantOK || rn.ok != wantOK {
		t.Fatalf("ok mismatch: %s %s hdr=%v wantOK=%v oldOK=%v newOK=%v", method, path, hdr, wantOK, ro.ok, rn.ok)
	}
	if wantOK && wantCluster != "" && (ro.cluster != wantCluster || rn.cluster != wantCluster) {
		t.Fatalf("cluster mismatch: %s %s hdr=%v want=%q old=%q new=%q", method, path, hdr, wantCluster, ro.cluster, rn.cluster)
	}
}

type varSyntax struct {
	simplePattern func(seg string) string         // /users/:id
	digitsPattern func(seg string) (string, bool) // /users/:id(\d+)
	multiPattern  func(a, b string) string        // /shops/:a/orders/:b
}

func colonSyntax() varSyntax {
	return varSyntax{
		simplePattern: func(seg string) string {
			return "/users/:" + seg
		},
		digitsPattern: func(seg string) (string, bool) {
			return "/users/:" + seg + "(\\d+)", true
		},
		multiPattern: func(a, b string) string {
			return "/shops/:" + a + "/orders/:" + b
		},
	}
}

/* ==============================
   test cases (var/regex/priority/header/)
   ============================== */

func TestParity_SimpleCases(t *testing.T) {
	syntax = colonSyntax()

	specs := []RouteSpec{
		// exact
		{ID: "exact", Methods: []string{"GET"}, Path: "/api/v1/item/100", Cluster: "c-exact"},
		// prefix（/**）
		{ID: "pre", Methods: []string{"GET"}, Prefix: "/api/v1/svc/", Cluster: "c-pre"},
		// var
		{ID: "var", Methods: []string{"GET"}, Path: syntax.simplePattern("id"), Cluster: "c-var"},
		// multi
		{ID: "multi", Methods: []string{"GET", "POST"}, Path: "/multi", Cluster: "c-multi"},
		// Header regex
		{ID: "hdr", Methods: []string{"GET"}, Headers: []HeaderSpec{{Name: "X-Env", Values: []string{"^prod|staging$"}, Regex: true}}, Cluster: "c-hdr"},
	}

	oldc := buildOld(specs)
	newc := buildNew(specs)

	cases := []struct {
		name    string
		method  string
		path    string
		hdr     map[string]string
		ok      bool
		cluster string
	}{
		{"exact", "GET", "/api/v1/item/100", nil, true, "c-exact"},
		{"prefix.deep", "GET", "/api/v1/svc/a/b", nil, true, "c-pre"},
		{"var.hit", "GET", "/users/42", nil, true, "c-var"},
		{"var.not_deeper", "GET", "/users/42/extra", nil, false, ""},
		{"multi.get", "GET", "/multi", nil, true, "c-multi"},
		{"multi.post", "POST", "/multi", nil, true, "c-multi"},
		{"hdr.regex", "GET", "/whatever", map[string]string{"X-Env": "prod"}, true, "c-hdr"},
		{"miss", "GET", "/no/match", nil, false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSame(t, oldc, newc, tc.method, tc.path, tc.hdr, tc.ok, tc.cluster)
		})
	}
}

func TestPriority_SpecificOverWildcard(t *testing.T) {
	syntax = colonSyntax()

	specs := []RouteSpec{
		{ID: "wild", Methods: []string{"GET"}, Prefix: "/api/v1/**", Cluster: "c-wild"},
		{ID: "spec", Methods: []string{"GET"}, Path: "/api/v1/test-dubbo/user/name/" + syntax.simplePattern("name")[len("/users/"):], Cluster: "c-spec"},
		// equals to /api/v1/test-dubbo/user/name/:name
	}
	oldc := buildOld(specs)
	newc := buildNew(specs)

	assertSame(t, oldc, newc, "GET",
		"/api/v1/test-dubbo/user/name/yqxu", nil, true, "c-spec")
}

func TestPriority_DeeperWins(t *testing.T) {
	specs := []RouteSpec{
		{ID: "shallow", Methods: []string{"GET"}, Prefix: "/api/v1/", Cluster: "c-shallow"},
		{ID: "deeper", Methods: []string{"GET"}, Prefix: "/api/v1/test-dubbo/", Cluster: "c-deeper"},
	}
	oldc := buildOld(specs)
	newc := buildNew(specs)

	assertSame(t, oldc, newc, "GET",
		"/api/v1/test-dubbo/user/name/abc", nil, true, "c-deeper")
}

func TestPriority_SingleStarOverDoubleStar(t *testing.T) {
	// use var to express "/*"
	syntax = colonSyntax()
	specs := []RouteSpec{
		{ID: "multi", Methods: []string{"GET"}, Prefix: "/api/", Cluster: "c-**"},
		{ID: "single", Methods: []string{"GET"}, Path: "/api/" + syntax.simplePattern("seg")[len("/users/"):] + "/users", Cluster: "c-*"},
		// equals to /api/:seg/users
	}
	oldc := buildOld(specs)
	newc := buildNew(specs)

	assertSame(t, oldc, newc, "GET", "/api/v1/users", nil, true, "c-*")
	assertSame(t, oldc, newc, "GET", "/api/v1/x/users", nil, true, "c-**")
}

func TestVariables_SingleAndMulti(t *testing.T) {
	syntax = colonSyntax()
	specs := []RouteSpec{
		{ID: "one", Methods: []string{"GET"}, Path: syntax.simplePattern("id"), Cluster: "c-one"},
		{ID: "two", Methods: []string{"GET"}, Path: syntax.multiPattern("shopId", "orderId"), Cluster: "c-two"},
		{ID: "pre", Methods: []string{"GET"}, Prefix: "/shops/", Cluster: "c-pre"},
	}
	oldc := buildOld(specs)
	newc := buildNew(specs)

	assertSame(t, oldc, newc, "GET", "/users/777", nil, true, "c-one")
	assertSame(t, oldc, newc, "GET", syntax.multiPattern("12", "34"), nil, true, "c-two")
	assertSame(t, oldc, newc, "GET", syntax.multiPattern("12", "34")+"/extra", nil, true, "c-pre")
}

func TestHeaderRegex_WithRoutes(t *testing.T) {
	specs := []RouteSpec{
		{ID: "hdr", Methods: []string{"GET"}, Headers: []HeaderSpec{{Name: "X-Env", Values: []string{"^prod|staging$"}, Regex: true}}, Cluster: "c-hdr"},
		{ID: "pre", Methods: []string{"GET"}, Prefix: "/api/", Cluster: "c-pre"},
	}
	oldc := buildOld(specs)
	newc := buildNew(specs)

	assertSame(t, oldc, newc, "GET", "/whatever", map[string]string{"X-Env": "prod"}, true, "c-hdr")
	assertSame(t, oldc, newc, "GET", "/api/foo", map[string]string{"X-Env": "dev"}, true, "c-pre")
}

/* ==============================
   random data fuzz test
   ============================== */

func TestParity_Randomized(t *testing.T) {
	syntax = colonSyntax()
	const (
		nRoutes           = 20000
		nRequests         = 10000
		prefixRatio       = 0.40
		headerRatio       = 0.10
		seed        int64 = 20250929
	)

	specs := genRandomSpecsWithVars(syntax, nRoutes, prefixRatio, headerRatio, seed)
	oldc := buildOld(specs)
	newc := buildNew(specs)

	reqs := genRandomRequests(nRequests, seed+1)
	for i, req := range reqs {
		ro, rn := call(oldc, newc, req.Method, req.URL.Path, headerFromReq(req))
		if ro.ok != rn.ok || ro.cluster != rn.cluster {
			t.Fatalf("Randomized mismatch at #%d: %s %s old={ok:%v cluster:%q err:%q} new={ok:%v cluster:%q err:%q}",
				i, req.Method, req.URL.Path, ro.ok, ro.cluster, ro.err, rn.ok, rn.cluster, rn.err)
		}
	}
}

func headerFromReq(r *http.Request) map[string]string {
	if len(r.Header) == 0 {
		return nil
	}
	out := make(map[string]string)
	for k, vs := range r.Header {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

/* ==============================
   random data generation tools
   ============================== */

var syntax varSyntax

func genRandomSpecsWithVars(s varSyntax, n int, prefixRatio, headerOnlyRatio float64, seed int64) []RouteSpec {
	rnd := rand.New(rand.NewSource(seed))
	out := make([]RouteSpec, 0, n)

	nHeader := int(float64(n) * headerOnlyRatio)
	nPrefix := int(float64(n-nHeader) * prefixRatio)
	// preserve 20% for "variable path", the rest for exact path
	nVars := int(float64(n) * 0.20)
	nPath := n - nHeader - nPrefix - nVars
	if nPath < 0 {
		nPath = 0
	}

	// Header-only (regex + normal)
	for i := 0; i < nHeader; i++ {
		if i%5 == 0 {
			out = append(out, RouteSpec{
				ID:      "hdrx-" + strconv.Itoa(i),
				Methods: []string{"GET", "POST"},
				Headers: []HeaderSpec{{Name: "X-Trace", Values: []string{"^pixiu-[0-9a-f]{8}$"}, Regex: true}},
				Cluster: "c-hx-" + strconv.Itoa(i),
			})
		} else {
			out = append(out, RouteSpec{
				ID:      "hdr-" + strconv.Itoa(i),
				Methods: []string{"GET", "POST"},
				Headers: []HeaderSpec{{Name: "X-Env", Values: []string{"prod"}, Regex: false}},
				Cluster: "c-h-" + strconv.Itoa(i),
			})
		}
	}

	// Prefix
	for i := 0; i < nPrefix; i++ {
		base := "/api/v" + strconv.Itoa(1+rnd.Intn(3)) + "/svc" + strconv.Itoa(rnd.Intn(50)) + "/"
		out = append(out, RouteSpec{
			ID:      "pre-" + strconv.Itoa(i),
			Methods: []string{"GET", "POST"},
			Prefix:  base,
			Cluster: "c-p-" + strconv.Itoa(i),
		})
	}

	// Variables
	for i := 0; i < nVars; i++ {
		if i%3 == 0 {
			out = append(out, RouteSpec{
				ID:      "var-" + strconv.Itoa(i),
				Methods: []string{"GET"},
				Path:    s.simplePattern("id"),
				Cluster: "c-v-" + strconv.Itoa(i),
			})
		} else {
			out = append(out, RouteSpec{
				ID:      "var2-" + strconv.Itoa(i),
				Methods: []string{"GET"},
				Path:    s.multiPattern("a", "b"),
				Cluster: "c-v2-" + strconv.Itoa(i),
			})
		}
	}

	// Exact Path
	for i := 0; i < nPath; i++ {
		out = append(out, RouteSpec{
			ID:      "pth-" + strconv.Itoa(i),
			Methods: []string{"GET"},
			Path:    "/api/v1/item/" + strconv.Itoa(i),
			Cluster: "c-x-" + strconv.Itoa(i),
		})
	}
	return out
}

func genRandomRequests(n int, seed int64) []*http.Request {
	rnd := rand.New(rand.NewSource(seed))
	reqs := make([]*http.Request, 0, n)
	methods := []string{"GET", "POST"}

	for i := 0; i < n; i++ {
		var path string
		switch rnd.Intn(5) {
		case 0: // exact style
			path = "/api/v1/item/" + strconv.Itoa(rnd.Intn(50000))
		case 1: // prefix style
			path = "/api/v" + strconv.Itoa(1+rnd.Intn(3)) + "/svc" + strconv.Itoa(rnd.Intn(50)) + "/foo/bar"
		case 2: // var
			path = "/users/" + strconv.Itoa(1000+rnd.Intn(9000))
		case 3: // var
			path = "/shops/" + strconv.Itoa(rnd.Intn(100)) + "/orders/" + strconv.Itoa(rnd.Intn(1000))
		default:
			path = "/unknown/" + strconv.Itoa(rnd.Intn(100000))
		}
		req, _ := http.NewRequest(methods[rnd.Intn(len(methods))], path, nil)
		// header-only
		switch rnd.Intn(7) {
		case 0:
			req.Header.Set("X-Env", "prod")
		case 1:
			req.Header.Set("X-Trace", "pixiu-"+strconv.FormatInt(rnd.Int63()&0xffffffff, 16))
		}
		reqs = append(reqs, req)
	}
	return reqs
}
