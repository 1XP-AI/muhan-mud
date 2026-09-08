import assert from "node:assert/strict";
import test from "node:test";
import { TerminalLine } from "./terminal-line.ts";

test("wait for prompt and never send pasted next-stage password", () => {
 const line=new TerminalLine();
 assert.deepEqual(line.input("ignored\r"),[]);
 line.prompt(false);
 assert.deepEqual(line.input("타봇\rpassword\r"),["타봇"]);
 assert.equal(line.text,"");
 assert.deepEqual(line.input("more\r"),[]);
});
test("secret input does not appear in display",()=>{
 const line=new TerminalLine();line.prompt(true);
 line.input("pw1234");assert.equal(line.display,"");
 assert.deepEqual(line.input("\r"),["pw1234"]);
});
test("erase Korean codepoint and ignore arrow sequence",()=>{
 const line=new TerminalLine();line.prompt(false);
 line.input("가나\x7f");assert.equal(line.display,"가");
 line.input("\x1b[A");assert.equal(line.display,"가");
 line.clear();assert.equal(line.text,"");
});
