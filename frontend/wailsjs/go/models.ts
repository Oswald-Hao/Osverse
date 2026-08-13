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
