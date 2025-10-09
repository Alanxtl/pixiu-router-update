/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package trie

import (
	"fmt"
	"strings"
)

import (
	"github.com/pkg/errors"
)

import (
	utils "github.com/alanxtl/pixiu-router-update/utils"
)

// Trie represents the Trie structure with the root node.
type Trie struct {
	root Node
}

// NewTrie creates and returns a new Trie.
func NewTrie() Trie {
	return Trie{root: Node{endOfPath: false, matchStr: ""}}
}

// NewTrieWithDefault creates a new Trie with a default path and value.
func NewTrieWithDefault(path string, defVal any) Trie {
	ret := Trie{root: Node{endOfPath: false, matchStr: ""}}
	_, _ = ret.Put(path, defVal)
	return ret
}

// Node represents each node in the Trie.
type Node struct {
	matchStr         string           // abc match abc, :a match all words as a variable names a , * match all words  ,** match all words and children.
	children         map[string]*Node // in path /a/b/c  , b is child of a , c is child of b
	PathVariablesSet map[string]*Node // in path /:a/b/c/:d , :a is a path variable node of level1 , :d is path variable node of level4
	PathVariableNode *Node            // in path /:a/b/c/:d , /b/c/:d is a child tree of pathVariable node :a ,and some special logic for match pathVariable it better not store in children.
	MatchAllNode     *Node            // /a/b/**  /** is a match all Node.
	endOfPath        bool             // if true means a real path exists ,  /a/b/c/d only node of d is true, a,b,c is false.
	bizInfo          any              // route info and any other info store here.
}

// Clear resets the Trie to its initial state.
func (trie *Trie) Clear() bool {
	return trie.root.Clear()
}

// IsEmpty checks if the Trie is empty.
func (trie *Trie) IsEmpty() bool {
	return trie.root.IsEmpty()
}

// Put adds a path and associated business information to the Trie.
func (trie *Trie) Put(withOutHost string, bizInfo any) (bool, error) {
	if bizInfo == nil {
		return false, errors.Errorf("data to put should not be nil.")
	}
	parts := utils.Split(withOutHost)
	return trie.root.internalPut(parts, bizInfo)
}

// PutOrUpdate updates a path and its business info in the Trie.
func (trie *Trie) PutOrUpdate(withOutHost string, bizInfo any) (bool, error) {
	if bizInfo == nil {
		return false, errors.Errorf("data to put should not be nil.")
	}
	parts := utils.Split(withOutHost)
	if _, err := trie.Remove(withOutHost); err != nil {
		fmt.Printf("PutOrUpdate failed for %s: %v", withOutHost, err)
	}
	return trie.root.internalPut(parts, bizInfo)
}

// Get retrieves the business info for a path.
func (trie *Trie) Get(withOutHost string) (*Node, []string, bool, error) {
	parts := utils.Split(withOutHost)
	node, param, ok, e := trie.root.Get(parts)
	length := len(param)
	for i := 0; i < length/2; i++ {
		temp := param[length-1-i]
		param[length-1-i] = param[i]
		param[i] = temp
	}
	return node, param, ok, e
}

// Match checks if the path matches any route in the Trie.
func (trie *Trie) Match(withOutHost string) (*Node, []string, bool) {
	withOutHost = strings.Split(withOutHost, "?")[0]
	parts := utils.Split(withOutHost)
	node, param, ok := trie.root.Match(parts)
	length := len(param)
	for i := 0; i < length/2; i++ {
		temp := param[length-1-i]
		param[length-1-i] = param[i]
		param[i] = temp
	}
	return node, param, ok
}

// Remove removes a path from the Trie.
func (trie *Trie) Remove(withOutHost string) (*Node, error) {
	n, _, _, e := trie.Get(withOutHost)
	if e != nil {
		return nil, e
	}
	if n != nil {
		n.endOfPath = false
	}
	return n, nil
}

// Contains checks if a key exists in the Trie.
func (trie *Trie) Contains(withOutHost string) (bool, error) {
	parts := utils.Split(withOutHost)
	ret, _, _, e := trie.root.Get(parts)
	if e != nil {
		return true, e
	}
	return !(ret == nil), nil
}

// internalPut is the internal logic to put a key and its bizInfo in the Trie
func (node *Node) internalPut(keys []string, bizInfo any) (bool, error) {
	// 初始化子节点
	if node.children == nil {
		node.children = make(map[string]*Node)
	}
	if len(keys) == 0 {
		return true, nil
	}

	key := keys[0]
	// isReal is the end of url path, means node is a place of url end,
	// so the path with parentNode has a real url exists.
	isReal := len(keys) == 1
	isSuccess := node.put(key, isReal, bizInfo)

	if !isSuccess {
		return false, nil
	}
	childKeys := keys[1:]

	// 如果是路径变量或通配符路径
	if utils.IsPathVariableOrWildcard(key) {
		return node.PathVariableNode.internalPut(childKeys, bizInfo)
	} else if utils.IsMatchAll(key) {
		return isSuccess, nil
	} else {
		return node.children[key].internalPut(childKeys, bizInfo)
	}
}

func (node *Node) Clear() bool {
	*node = Node{}
	return true
}

// IsEmpty return true if empty
func (node *Node) IsEmpty() bool {
	if node.children == nil && node.matchStr == "" && node.PathVariableNode == nil && node.PathVariablesSet == nil {
		return true
	}
	return false
}

// GetBizInfo get info
func (node *Node) GetBizInfo() any {
	return node.bizInfo
}

//Match node match

func (node *Node) Match(parts []string) (*Node, []string, bool) {
	key := parts[0]
	childKeys := parts[1:]
	// isEnd is the end of url path, means node is a place of url end,so the path with parentNode has a real url exists.
	isEnd := len(childKeys) == 0
	if isEnd {

		if node.children != nil && node.children[key] != nil && node.children[key].endOfPath {
			return node.children[key], []string{}, true
		}
		//consider  trie node ：/aaa/bbb/xxxxx/ccc/ddd  /aaa/bbb/:id/ccc   and request url is ：/aaa/bbb/xxxxx/ccc
		if node.PathVariableNode != nil {
			if node.PathVariableNode.endOfPath {
				return node.PathVariableNode, []string{key}, true
			}
		}

	} else {
		if node.children != nil && node.children[key] != nil {
			n, param, ok := node.children[key].Match(childKeys)
			if ok {
				return n, param, ok
			}
		}
		if node.PathVariableNode != nil {
			n, param, ok := node.PathVariableNode.Match(childKeys)
			param = append(param, key)
			if ok {
				return n, param, ok
			}
		}
	}
	if node.children != nil && node.children[key] != nil && node.children[key].MatchAllNode != nil {
		return node.children[key].MatchAllNode, []string{}, true
	}
	if node.MatchAllNode != nil {
		return node.MatchAllNode, []string{}, true
	}
	return nil, nil, false
}

// Get node get
// returns:
// *Node this node in path, if not exists return nil
// []string key reversed array of pathVariable   /:aa/:bb/:cc  returns array of (cc,bb,aa)
// bool is ok
// error
func (node *Node) Get(keys []string) (*Node, []string, bool, error) {
	key := keys[0]
	childKeys := keys[1:]
	isReal := len(childKeys) == 0
	if isReal {
		// exit condition
		if utils.IsPathVariableOrWildcard(key) {
			if node.PathVariableNode == nil || !node.PathVariableNode.endOfPath {
				return nil, nil, false, nil
			}
			return node.PathVariableNode, []string{utils.VariableName(key)}, true, nil
		} else if utils.IsMatchAll(key) {
			return node.MatchAllNode, nil, true, nil
		} else {
			if node.children == nil {
				return nil, nil, false, nil
			}
			return node.children[key], nil, true, nil
		}
	} else {

		if utils.IsPathVariableOrWildcard(key) {
			if node.PathVariableNode == nil {
				return nil, nil, false, nil
			}
			retNode, pathVariableList, ok, e := node.PathVariableNode.Get(childKeys)
			newList := []string{key}
			copy(newList[1:], pathVariableList)
			return retNode, newList, ok, e
		} else if utils.IsMatchAll(key) {
			return nil, nil, false, errors.Errorf("router configuration is empty")
		} else {
			if node.children == nil || node.children[key] == nil {
				return nil, nil, false, nil
			}
			return node.children[key].Get(childKeys)
		}
	}

}

func (node *Node) put(key string, isReal bool, bizInfo any) bool {
	if !utils.IsPathVariableOrWildcard(key) {
		if utils.IsMatchAll(key) {
			return node.putMatchAllNode(key, isReal, bizInfo)
		} else {
			return node.putNode(key, isReal, bizInfo)
		}
	}
	pathVariable := utils.VariableName(key)
	return node.putPathVariable(pathVariable, isReal, bizInfo)
}

func (node *Node) putPathVariable(pathVariable string, isReal bool, bizInfo any) bool {
	//path variable put
	if node.PathVariableNode == nil {
		node.PathVariableNode = &Node{endOfPath: false}
	}
	if node.PathVariableNode.endOfPath && isReal {
		//has a node with same path exists. conflicted.
		return false
	}
	if isReal {
		node.PathVariableNode.bizInfo = bizInfo
		node.PathVariableNode.matchStr = pathVariable
	}
	node.PathVariableNode.endOfPath = node.PathVariableNode.endOfPath || isReal
	if node.PathVariablesSet == nil {
		node.PathVariablesSet = map[string]*Node{}
	}
	node.PathVariablesSet[pathVariable] = node.PathVariableNode
	return true
}

func (node *Node) putNode(matchStr string, isReal bool, bizInfo any) bool {
	selfNode := &Node{endOfPath: isReal, matchStr: matchStr}
	old := node.children[matchStr]
	if old != nil {
		if old.endOfPath && isReal {
			// already has one same path url
			return false
		}
		selfNode = old
	} else {
		old = selfNode
	}

	if isReal {
		selfNode.bizInfo = bizInfo
	}
	selfNode.endOfPath = isReal || old.endOfPath
	node.children[matchStr] = selfNode
	return true
}

func (node *Node) putMatchAllNode(matchStr string, isReal bool, bizInfo any) bool {
	selfNode := &Node{endOfPath: isReal, matchStr: matchStr}
	old := node.MatchAllNode
	if old != nil {
		if old.endOfPath && isReal {
			// already has one same path url
			return false
		}
		selfNode = old
	} else {
		old = selfNode
	}

	if isReal {
		selfNode.bizInfo = bizInfo
	}
	selfNode.endOfPath = selfNode.endOfPath || old.endOfPath
	node.MatchAllNode = selfNode
	return true
}
