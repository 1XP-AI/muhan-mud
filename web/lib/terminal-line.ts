/** One outstanding line at a time: never replay buffered input after a prompt. */
export class TerminalLine {
 text="";
 private ready=false;
 private secret=false;
 get display(){return this.secret ? "" : this.text;}
 get hidden(){return this.secret;}
 prompt(secret:boolean){this.text="";this.secret=secret;this.ready=true;}
 clear(){this.text="";this.ready=false;}
 input(data:string):string[]{
  if(!this.ready || data.startsWith("\x1b")) return [];
  for(const char of data){
   if(char==="\r" || char==="\n"){
    const submitted=this.text;this.clear();return [submitted];
   }
   if(char==="\x7f" || char==="\b"){this.text=Array.from(this.text).slice(0,-1).join("");continue;}
   if(char.codePointAt(0)!<32)continue;
   if(new TextEncoder().encode(this.text+char).length<=512)this.text+=char;
  }
  return [];
 }
}
