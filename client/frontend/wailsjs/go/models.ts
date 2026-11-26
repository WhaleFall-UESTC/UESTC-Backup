export namespace main {
	
	export class JobReply {
	    code: number;
	    message?: string;
	    job_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new JobReply(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.message = source["message"];
	        this.job_id = source["job_id"];
	    }
	}
	export class Reply {
	    code: number;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new Reply(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.message = source["message"];
	    }
	}
	export class TasksReply {
	    code: number;
	    message?: string;
	    tasks: tasks.Task[];
	
	    static createFrom(source: any = {}) {
	        return new TasksReply(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.message = source["message"];
	        this.tasks = this.convertValues(source["tasks"], tasks.Task);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace manager {
	
	export class SkipInfo {
	    path: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new SkipInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.reason = source["reason"];
	    }
	}
	export class Result {
	    files: number;
	    renamed: number;
	    skipped: number;
	    bytes: number;
	    skips: SkipInfo[];
	    unsupported: string[];
	    code: number;
	    message: string;
	    duration: number;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.files = source["files"];
	        this.renamed = source["renamed"];
	        this.skipped = source["skipped"];
	        this.bytes = source["bytes"];
	        this.skips = this.convertValues(source["skips"], SkipInfo);
	        this.unsupported = source["unsupported"];
	        this.code = source["code"];
	        this.message = source["message"];
	        this.duration = source["duration"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class Snapshot {
	    id: string;
	    kind: string;
	    state: string;
	    task: string;
	    phase: string;
	    path: string;
	    done_files: number;
	    total_files: number;
	    done_bytes: number;
	    total_bytes: number;
	    result?: Result;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.state = source["state"];
	        this.task = source["task"];
	        this.phase = source["phase"];
	        this.path = source["path"];
	        this.done_files = source["done_files"];
	        this.total_files = source["total_files"];
	        this.done_bytes = source["done_bytes"];
	        this.total_bytes = source["total_bytes"];
	        this.result = this.convertValues(source["result"], Result);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace tasks {
	
	export class Task {
	    name: string;
	    source: string;
	    destination: string;
	    algorithm: string;
	    encrypted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.source = source["source"];
	        this.destination = source["destination"];
	        this.algorithm = source["algorithm"];
	        this.encrypted = source["encrypted"];
	    }
	}

}

