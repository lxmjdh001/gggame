// Match the server's existing UTF-8 byte-length semantics, not JS UTF-16 units.
export function validNewPasswordLength(password:string):boolean{
 const length=new TextEncoder().encode(password).length
 return length>=8&&length<=16
}
