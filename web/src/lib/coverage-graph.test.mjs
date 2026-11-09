import assert from "node:assert/strict";
import test from "node:test";
import { computeVisible } from "./coverage-graph.ts";

const node = (key, kind = "subdomain", tested = false) => ({ key, kind, label: key, tested, in_scope: true });
const keys = (view) => view.renderNodes.filter(n=>!n.fold).map(n=>n.key);

test("folds retain every sibling and expand without silently truncating descendants", () => {
  const nodes = [node("root","company"),...Array.from({length:50},(_,i)=>node(String(i).padStart(2,"0"),"subdomain",i%3===0)),node("leaf","endpoint")];
  const edges = [...nodes.slice(1,51).map(n=>({src:n.key,dst:"root"})),{src:"leaf",dst:"49"}];
  const first = computeVisible(nodes,edges,new Map());
  const fold = first.renderNodes.find(n=>n.fold);
  assert.equal(keys(first).length,21);
  assert.equal(fold.hidden.length,30);
  assert.equal(new Set([...keys(first),...fold.hidden.map(n=>n.key)]).size,51);
  assert.ok(first.renderNodes.filter(n=>!n.fold&&n.node.tested).length===17);
  const all = computeVisible(nodes,edges,new Map([["root::subdomain",100]]));
  assert.equal(keys(all).length,52);
  assert.equal(all.renderNodes.filter(n=>n.fold).length,0);
  assert.equal(all.renderEdges.length,51);
});

test("rootless cycles, self loops, disconnected nodes and deep chains stay reachable", () => {
  const cycle = computeVisible([node("a"),node("b"),node("self"),node("alone")],[{src:"a",dst:"b"},{src:"b",dst:"a"},{src:"self",dst:"self"}],new Map());
  assert.deepEqual(new Set(keys(cycle)),new Set(["a","b","self","alone"]));
  assert.equal(cycle.renderEdges.length,3);
  const count=20000;
  const deep=computeVisible(Array.from({length:count},(_,i)=>node(String(i))),Array.from({length:count-1},(_,i)=>({src:String(i+1),dst:String(i)})),new Map());
  assert.equal(keys(deep).length,count);
  assert.equal(deep.renderEdges.length,count-1);
});

test("duplicate and missing edges do not inflate folded counts or leak dangling links", () => {
  const view=computeVisible([node("root","company"),...Array.from({length:25},(_,i)=>node(String(i)))],Array.from({length:25},(_,i)=>[{src:String(i),dst:"root"},{src:String(i),dst:"root"}]).flat().concat([{src:"missing",dst:"root"},{src:"root",dst:"missing"}]),new Map());
  assert.equal(view.renderNodes.find(n=>n.fold).hidden.length,5);
  assert.equal(view.renderEdges.length,21);
  assert.equal(new Set(view.renderEdges.map(e=>JSON.stringify(e))).size,view.renderEdges.length);
});

test("an asset visible through another parent is not repeated in hidden inventory", () => {
  const nodes=[node("p1","company"),node("p2","company"),...Array.from({length:21},(_,i)=>node(String(i).padStart(2,"0")))];
  const edges=[...nodes.slice(2).map(n=>({src:n.key,dst:"p1"})),{src:"20",dst:"p2"}];
  const view=computeVisible(nodes,edges,new Map());
  assert.equal(keys(view).length,nodes.length);
  assert.equal(view.renderNodes.some(n=>n.fold),false);
  assert.equal(view.renderEdges.length,edges.length);
});
