export interface SecondaryPasswordStatus {
 is_set:boolean
 enabled:boolean
 verification_required:boolean
 verified_until?:string
 server_time:string
}

export function validSecondaryPassword(password:string):boolean {
 const length=Array.from(password).length
 return length>=6&&length<=12
}

// Use the server's clock and fixed expiry. Polling/navigation must not renew it.
export function gameAccessTTL(status:SecondaryPasswordStatus):number {
 if(typeof status?.enabled!=='boolean'||typeof status?.is_set!=='boolean'||typeof status?.verification_required!=='boolean')throw new Error('二级密码状态读取失败，请重试')
 if(!status.enabled)return Infinity
 if(!status.is_set||status.verification_required)return 0
 const until=Date.parse(status.verified_until??''),now=Date.parse(status.server_time)
 if(!Number.isFinite(until)||!Number.isFinite(now))throw new Error('二级密码有效期读取失败，请重试')
 return Math.max(0,Math.min(3600000,until-now))
}
