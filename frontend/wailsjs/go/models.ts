export namespace domain {

	export class Installation {
	    path: string;
	    resolvedPath: string;
	    version: string;
	    source: string;
	    managed: boolean;

	    static createFrom(source: any = {}) {
	        return new Installation(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.resolvedPath = source["resolvedPath"];
	        this.version = source["version"];
	        this.source = source["source"];
	        this.managed = source["managed"];
	    }
	}
	export class Component {
	    id: string;
	    name: string;
	    category: string;
	    status: string;
	    installations: Installation[];
	    message: string;
	    minimumOS: string;

	    static createFrom(source: any = {}) {
	        return new Component(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.category = source["category"];
	        this.status = source["status"];
	        this.installations = this.convertValues(source["installations"], Installation);
	        this.message = source["message"];
	        this.minimumOS = source["minimumOS"];
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
	export class SystemInfo {
	    distribution: string;
	    version: string;
	    architecture: string;
	    shell: string;
	    supported: boolean;
	    unsupportedReason: string;

	    static createFrom(source: any = {}) {
	        return new SystemInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.distribution = source["distribution"];
	        this.version = source["version"];
	        this.architecture = source["architecture"];
	        this.shell = source["shell"];
	        this.supported = source["supported"];
	        this.unsupportedReason = source["unsupportedReason"];
	    }
	}
	export class EnvironmentSnapshot {
	    // Go type: time
	    scannedAt: any;
	    system: SystemInfo;
	    components: Component[];
	    ready: number;
	    total: number;
	    needsAttention: number;

	    static createFrom(source: any = {}) {
	        return new EnvironmentSnapshot(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scannedAt = this.convertValues(source["scannedAt"], null);
	        this.system = this.convertValues(source["system"], SystemInfo);
	        this.components = this.convertValues(source["components"], Component);
	        this.ready = source["ready"];
	        this.total = source["total"];
	        this.needsAttention = source["needsAttention"];
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

export namespace install {

	export class PlannedChange {
	    kind: string;
	    path: string;
	    description: string;

	    static createFrom(source: any = {}) {
	        return new PlannedChange(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.path = source["path"];
	        this.description = source["description"];
	    }
	}
	export class Plan {
	    id: string;
	    componentId: string;
	    name: string;
	    command: string;
	    version: string;
	    downloadBytes: number;
	    changes: PlannedChange[];
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    expiresAt: any;

	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.componentId = source["componentId"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.version = source["version"];
	        this.downloadBytes = source["downloadBytes"];
	        this.changes = this.convertValues(source["changes"], PlannedChange);
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.expiresAt = this.convertValues(source["expiresAt"], null);
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

export namespace proxy {

	export class Attempt {
	    protocol: string;
	    available: boolean;
	    latencyMillis: number;
	    message: string;

	    static createFrom(source: any = {}) {
	        return new Attempt(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.protocol = source["protocol"];
	        this.available = source["available"];
	        this.latencyMillis = source["latencyMillis"];
	        this.message = source["message"];
	    }
	}
	export class Result {
	    port: number;
	    reachable: boolean;
	    recommended: string;
	    attempts: Attempt[];
	    // Go type: time
	    checkedAt: any;

	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.port = source["port"];
	        this.reachable = source["reachable"];
	        this.recommended = source["recommended"];
	        this.attempts = this.convertValues(source["attempts"], Attempt);
	        this.checkedAt = this.convertValues(source["checkedAt"], null);
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
