"use client";

import { useEffect,useRef } from "react";
import { TerminalLine } from "@/lib/terminal-line";
import styles from "./classic-terminal.module.css";

export function ClassicTerminal({url}:{url:string|null}){
 const host=useRef<HTMLDivElement>(null);
 useEffect(()=>{
  let disposed=false;
  let cleanup=()=>{};
  async function start(){
   const [{Terminal},{FitAddon}]=await Promise.all([import("@xterm/xterm"),import("@xterm/addon-fit")]);
   if(disposed || !host.current)return;
   const term=new Terminal({fontFamily:'"D2Coding", "Noto Sans Mono CJK KR", monospace',fontSize:16,cursorBlink:false,convertEol:true,scrollback:2000,screenReaderMode:true,theme:{background:"#000080",foreground:"#e0e0e0",cursor:"#ffffff",selectionBackground:"#00ffff55"}});
   const fit=new FitAddon();term.loadAddon(fit);term.open(host.current);fit.fit();
   const line=new TerminalLine();
   let socket:WebSocket|undefined;
   let composing=false;
   const focus=()=>{if(!disposed && !composing && !term.hasSelection() && !window.getSelection()?.toString())term.focus();};
   const resize=()=>{if(!disposed && !composing){
    if(host.current)host.current.style.height=window.matchMedia("(max-width:640px)").matches ? `${window.visualViewport?.height ?? window.innerHeight}px` : "";
    fit.fit();
   }};
   const compositionStart=()=>{composing=true;};
   const compositionEnd=()=>{composing=false;};
   const element=host.current;
   element.addEventListener("compositionstart",compositionStart);
   element.addEventListener("compositionend",compositionEnd);
   element.addEventListener("pointerup",focus);
   window.addEventListener("focus",focus);
   window.visualViewport?.addEventListener("resize",resize);
   const observer=new ResizeObserver(resize);observer.observe(element);
   term.attachCustomKeyEventHandler(event=>event.key!=="Tab");
   const data=term.onData(value=>{
    if(!socket || socket.readyState!==WebSocket.OPEN)return;
    const before=line.display;
    const submissions=line.input(value);
    if(submissions.length){term.write("\x1b8\x1b[J"+(line.hidden ? "" : submissions[0])+"\r\n");socket.send(JSON.stringify({type:"line",text:submissions[0]}));}
    else if(line.display!==before){term.write("\x1b8\x1b[J"+line.display);}
   });
   term.writeln("무한대전 · 터미널 접속\r\n");
   if(!url){term.writeln("게임 서버 주소가 설정되지 않았습니다.");}
   else {
    try{
     const address=new URL(url);
     if(!["ws:","wss:"].includes(address.protocol) || (location.protocol==="https:" && address.protocol!=="wss:"))throw new Error("invalid transport");
     socket=new WebSocket(address);
     socket.onmessage=event=>{
      if(disposed)return;
      try{
       const view=JSON.parse(event.data);
       if(view.type!=="view" || typeof view.text!=="string" || typeof view.secret!=="boolean" || typeof view.closed!=="boolean")throw new Error("invalid view");
       line.clear();
       term.write(view.text+"\x1b7",()=>{if(!disposed && !view.closed)line.prompt(view.secret);});
      }catch{line.clear();socket?.close();}
     };
     socket.onclose=()=>{line.clear();if(!disposed)term.writeln("\r\n접속이 끝났습니다. 다시 접속하려면 페이지를 새로고침하십시오.");};
     socket.onerror=()=>{line.clear();if(!disposed)term.writeln("\r\n게임 서버에 연결하지 못했습니다.");};
    }catch{term.writeln("게임 서버 주소를 확인하십시오.");}
   }
   focus();
   cleanup=()=>{data.dispose();observer.disconnect();window.removeEventListener("focus",focus);window.visualViewport?.removeEventListener("resize",resize);element.removeEventListener("pointerup",focus);element.removeEventListener("compositionstart",compositionStart);element.removeEventListener("compositionend",compositionEnd);line.clear();socket?.close();term.dispose();};
  }
  void start();
  return()=>{disposed=true;cleanup();};
 },[url]);
 return <main className={styles.desktop}><div className={styles.terminal} ref={host} aria-label="무한대전 게임 터미널" /></main>;
}
