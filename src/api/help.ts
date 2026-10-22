export interface HelpArticleInput {title:string;content:string;enabled:boolean;sort:number;version:number}
export interface HelpArticle extends HelpArticleInput {id:string;updated_at:string}
