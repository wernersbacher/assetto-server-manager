import {
    RaceControl as RaceControlData,
    RaceControlDriverMapRaceControlDriver as Driver,
    RaceControlDriverMapRaceControlDriverSessionCarInfo as SessionCarInfo
} from "./models/RaceControl";

import {CarUpdate, CarUpdateVec} from "./models/UDP";
import {randomColor} from "randomcolor/randomColor";
import {msToTime, prettifyName} from "./utils";
import {t, tn} from "./i18n";
import moment from "moment";
import ReconnectingWebSocket from "reconnecting-websocket";
import ClickEvent = JQuery.ClickEvent;
import ChangeEvent = JQuery.ChangeEvent;

interface WSMessage {
    Message: any;
    EventType: number;
}

const EventCollisionWithCar = 10,
    EventCollisionWithEnv = 11,
    EventNewSession = 50,
    EventNewConnection = 51,
    EventConnectionClosed = 52,
    EventCarUpdate = 53,
    EventCarInfo = 54,
    EventEndSession = 55,
    EventVersion = 56,
    EventChat = 57,
    EventClientLoaded = 58,
    EventSessionInfo = 59,
    EventError = 60,
    EventLapCompleted = 73,
    EventClientEvent = 130,
    EventRaceControl = 200
;

interface SimpleCollision {
    WorldPos: CarUpdateVec
}

interface WebsocketHandler {
    handleWebsocketMessage(message: WSMessage): void;

    onTrackChange(track: string, trackLayout: string): void;
}

export class RaceControl {
    private readonly liveMap: LiveMap = new LiveMap(this);
    private readonly liveTimings: LiveTimings = new LiveTimings(this, this.liveMap);
    private readonly eventFeed: EventFeed = new EventFeed(this, this.liveMap);
    private readonly $eventTitle: JQuery<HTMLHeadElement>;
    public status: RaceControlData;
    private firstLoad: boolean = true;

    private track: string = "";
    private trackLayout: string = "";

    constructor() {
        this.$eventTitle = $("#event-title");
        this.status = new RaceControlData();

        if (!this.$eventTitle.length) {
            return;
        }

        let ws = new ReconnectingWebSocket(((window.location.protocol === "https:") ? "wss://" : "ws://") + window.location.host + "/api/race-control" + window.location.search, [], {
            minReconnectionDelay: 0,
        });

        ws.onmessage = this.handleWebsocketMessage.bind(this);

        $(window).on('beforeunload', () => {
            ws.close();
        });

        this.handleIFrames();
        this.handleTabs();
        this.handleAdminDrawer();
        setInterval(this.showEventCompletion.bind(this), 1000);
    }

    private handleWebsocketMessage(ev: MessageEvent): void {
        let message = JSON.parse(ev.data) as WSMessage;

        if (!message) {
            return;
        }

        switch (message.EventType) {
            case EventVersion:
                location.reload();
                return;
            case EventRaceControl:
                this.status = new RaceControlData(message.Message);

                if (this.status.SessionInfo.Track !== this.track || this.status.SessionInfo.TrackConfig !== this.trackLayout) {
                    this.track = this.status.SessionInfo.Track;
                    this.trackLayout = this.status.SessionInfo.TrackConfig;
                    this.liveMap.onTrackChange(this.track, this.trackLayout);
                    this.liveTimings.onTrackChange(this.track, this.trackLayout);
                    this.onTrackChange(this.track, this.trackLayout);
                }

                this.$eventTitle.text(t("live.event_title", {session: RaceControl.getSessionType(this.status.SessionInfo.Type), track: this.status.TrackInfo!.name}));
                $("#track-location").text(this.status.TrackInfo.city + ", " + this.status.TrackInfo.country);

                this.buildSessionInfo();

                this.firstLoad = false;
                break;
            case EventNewSession:
                break;
        }

        this.liveMap.handleWebsocketMessage(message);
        this.liveTimings.handleWebsocketMessage(message);
        this.eventFeed.handleWebsocketMessage(message);
    }

    private static getSessionType(sessionIndex: number): string {
        switch (sessionIndex) {
            case 0:
                return t("live.session.booking");
            case 1:
                return t("live.session.practice");
            case 2:
                return t("live.session.qualifying");
            case 3:
                return t("live.session.race");
            default:
                return t("live.session.unknown");
        }
    }

    private showEventCompletion() {
        let timeRemaining = "";

        // Get lap/laps or time/totalTime
        if (this.status.SessionInfo.Time > 0) {
            let timeInMS = (this.status.SessionInfo.Time * 60 * 1000) + (this.status.SessionInfo.WaitTime/126.166667 * 1000) - moment.duration(moment().utc().diff(moment(this.status.SessionStartTime).utc())).asMilliseconds();

            let days = Math.floor(timeInMS/8.64e+7);

            timeRemaining = msToTime(timeInMS, false, false);

            if (days > 0) {
                timeRemaining = tn("live.days_plus", days, {time: timeRemaining});
            }
        } else if (this.status.SessionInfo.Laps > 0) {
            let lapsCompleted = 0;

            if (this.status.ConnectedDrivers && this.status.ConnectedDrivers.GUIDsInPositionalOrder.length > 0) {
                let driver = this.status.ConnectedDrivers.Drivers[this.status.ConnectedDrivers.GUIDsInPositionalOrder[0]];

                if (driver.TotalNumLaps > 0) {
                    lapsCompleted = driver.TotalNumLaps;
                }
            }

            timeRemaining = tn("live.laps_remaining", this.status.SessionInfo.Laps - lapsCompleted);
        }

        let $raceTime = $("#race-time");
        $raceTime.text(timeRemaining);

        this.showEventProgress();
    }

    // showEventProgress fills the progress bar below the remaining time (elapsed share of the time or of the laps).
    private showEventProgress(): void {
        let progress = 0;

        if (this.status.SessionInfo.Time > 0) {
            const totalMS = (this.status.SessionInfo.Time * 60 * 1000) + (this.status.SessionInfo.WaitTime / 126.166667 * 1000);
            const elapsedMS = moment.duration(moment().utc().diff(moment(this.status.SessionStartTime).utc())).asMilliseconds();

            progress = totalMS > 0 ? elapsedMS / totalMS : 0;
        } else if (this.status.SessionInfo.Laps > 0 && this.status.ConnectedDrivers && this.status.ConnectedDrivers.GUIDsInPositionalOrder.length > 0) {
            const leader = this.status.ConnectedDrivers.Drivers[this.status.ConnectedDrivers.GUIDsInPositionalOrder[0]];

            if (leader && leader.TotalNumLaps > 0) {
                progress = leader.TotalNumLaps / this.status.SessionInfo.Laps;
            }
        }

        const percent = Math.round(Math.min(Math.max(progress, 0), 1) * 100);

        $("#race-progress").css("width", percent + "%");
        $("#race-progress-bar").attr("aria-valuenow", percent);
    }

    public onTrackChange(track: string, layout: string): void {
        const info: {[key: string]: any} = {
            "description": this.status.TrackInfo.description,
            "length": this.status.TrackInfo["length"],
            "pitboxes": this.status.TrackInfo.pitboxes,
            "width": this.status.TrackInfo.width,
            "run": this.status.TrackInfo.run,
        };

        // the fixed info line only shows what the track provides
        for (const key in info) {
            const value = info[key];

            $("#track-" + key).text(value);
            $("#track-info [data-info='" + key + "']").toggle(value !== undefined && value !== null && value !== "");
        }
    }

    private buildSessionInfo() {
        $("#road-temp-text").text(this.status.SessionInfo.RoadTemp + "°C");
        $("#ambient-temp-text").text(this.status.SessionInfo.AmbientTemp + "°C");
        $("#weather-text").text(RaceControl.getWeatherName(this.status.SessionInfo.WeatherGraphics));

        $("#event-name").text(this.status.SessionInfo.Name);
        $("#event-type").text(RaceControl.getSessionType(this.status.SessionInfo.Type));

        this.highlightSession();
    }

    // getWeatherName is the readable name of a weather preset.
    private static getWeatherName(weatherGraphics: string): string {
        // Fix for sol weathers with time info in this format:
        // sol_05_Broken%20Clouds_type=18_time=0_mult=20_start=1551792960/preview.jpg
        const parts = (weatherGraphics || "").split("_");

        for (let i = 0; i < parts.length; i++) {
            if (parts[i].indexOf("type=") !== -1) {
                parts.splice(i);
                break;
            }
        }

        return prettifyName(parts.join("_"), false) || "-";
    }

    // highlightSession marks the current session in the stepper and the ones before it as done.
    private highlightSession(): void {
        const keys = RaceControl.sessionKeys(this.status.SessionInfo.Type);
        const $steps = $(".live-step");
        let current = -1;

        $steps.each(function (i, el) {
            if (current === -1 && keys.indexOf(String($(el).data("session")).toUpperCase()) !== -1) {
                current = i;
            }
        });

        $steps.each(function (i, el) {
            $(el).toggleClass("is-active", i === current)
                .toggleClass("is-done", current !== -1 && i < current);

            if (i === current) {
                $(el).attr("aria-current", "step");
            } else {
                $(el).removeAttr("aria-current");
            }
        });
    }

    private static sessionKeys(sessionIndex: number): string[] {
        switch (sessionIndex) {
            case 0:
                return ["BOOK"];
            case 1:
                return ["PRACTICE"];
            case 2:
                return ["QUALIFY"];
            case 3:
                return ["RACE", "RACEX2"];
            default:
                return [];
        }
    }

    private handleTabs(): void {
        // on small screens standings and map/chat are shown one at a time
        $(document).on("click", ".live-tab", (e: ClickEvent) => {
            const tab = $(e.currentTarget).data("live-tab") as string;

            $(".live-tab").removeClass("active").attr("aria-selected", "false");
            $(e.currentTarget).addClass("active").attr("aria-selected", "true");
            $(".live-panes").attr("data-active-tab", tab);

            // the live map calculates its scale from the visible image size
            $(window).trigger("resize");
        });
    }

    // handleAdminDrawer opens and closes the admin drawer (admins only, the button is not rendered otherwise).
    private handleAdminDrawer(): void {
        const $drawer = $("#admin-drawer");
        const $backdrop = $("#admin-drawer-backdrop");
        const $button = $("#admin-panel");

        if (!$drawer.length) {
            return;
        }

        const setOpen = (open: boolean) => {
            $drawer.toggleClass("open", open).attr("aria-hidden", open ? "false" : "true");
            $backdrop.prop("hidden", !open);
            $("body").toggleClass("live-drawer-open", open);
            $button.attr("aria-expanded", open ? "true" : "false");

            if (open) {
                $drawer.find("input, select").first().trigger("focus");
            } else {
                $button.trigger("focus");
            }
        };

        $button.on("click", () => setOpen(!$drawer.hasClass("open")));
        $("#admin-drawer-close").on("click", () => setOpen(false));
        $backdrop.on("click", () => setOpen(false));

        $(document).on("keydown", (e: JQuery.KeyDownEvent) => {
            if (e.key === "Escape" && $drawer.hasClass("open")) {
                setOpen(false);
            }
        });
    }

    private handleIFrames(): void {
        const $document = $(document);

        $document.on("change", ".live-frame-link", function (e: ChangeEvent) {
            let $this = $(e.currentTarget) as JQuery<HTMLInputElement>;
            let value = $this.val() as string;

            if (value) {
                let $liveTimingFrame = $this.closest(".live-frame-wrapper").find(".live-frame");
                $this.closest(".live-frame-wrapper").find(".embed-responsive").attr("class", "embed-responsive embed-responsive-16by9");

                // if somebody pasted an embed code just grab the actual link
                if (value.startsWith('<iframe')) {
                    let res = value.split('"');

                    for (let i = 0; i < res.length; i++) {
                        if (res[i] === " src=") {
                            if (res[i + 1]) {
                                $liveTimingFrame.attr("src", res[i + 1]);
                            }

                            $this.val(res[i + 1]);
                        }
                    }
                } else {
                    $liveTimingFrame.attr("src", value);
                }
            }
        });

        $document.on("click", ".remove-live-frame", function (e: ClickEvent) {
            $(e.currentTarget).closest(".live-frame-wrapper").remove();
        });

        $document.find("#add-live-frame").click(function () {
            let $copy = $document.find(".live-frame-wrapper").first().clone();

            $copy.removeClass("d-none");
            $copy.find(".embed-responsive").attr("class", "d-none embed-responsive embed-responsive-16by9");
            $copy.find(".frame-input").removeClass("ml-0");

            $document.find(".live-frame-wrapper").last().after($copy);
        });
    }
}

declare var useMPH: boolean;

class LiveMap implements WebsocketHandler {
    private mapImageHasLoaded: boolean = false;

    private readonly $map: JQuery<HTMLDivElement>;
    private readonly $trackMapImage: JQuery<HTMLImageElement>;
    private readonly raceControl: RaceControl;

    constructor(raceControl: RaceControl) {
        this.$map = $("#map");
        this.raceControl = raceControl;
        this.$trackMapImage = this.$map.find("img") as JQuery<HTMLImageElement>;

        $(window).on("resize", this.correctMapDimensions.bind(this));
    }

    // positional coordinate modifiers.
    private mapScaleMultiplier: number = 1;
    private trackScale: number = 1;
    private trackMargin: number = 0;
    private trackXOffset: number = 0;
    private trackZOffset: number = 0;

    // live map track dots
    private dots: Map<string, JQuery<HTMLElement>> = new Map<string, JQuery<HTMLElement>>();
    private maxRPMs: Map<string, number> = new Map<string, number>();

    public handleWebsocketMessage(message: WSMessage): void {
        switch (message.EventType) {
            case EventRaceControl:
                this.trackXOffset = this.raceControl.status.TrackMapData!.offset_x;
                this.trackZOffset = this.raceControl.status.TrackMapData!.offset_y;
                this.trackScale = this.raceControl.status.TrackMapData!.scale_factor;

                for (const connectedGUID in this.raceControl.status.ConnectedDrivers!.Drivers) {
                    const driver = this.raceControl.status.ConnectedDrivers!.Drivers[connectedGUID];

                    if (!this.dots.has(driver.CarInfo.DriverGUID)) {
                        // in the event that a user just loaded the race control page, place the
                        // already loaded dots onto the map
                        let $driverDot = this.buildDriverDot(driver.CarInfo, driver.LastPos as CarUpdateVec).show();
                        this.dots.set(driver.CarInfo.DriverGUID, $driverDot);
                    }
                }

                $(".dot").css({"transition": this.raceControl.status.CurrentRealtimePosInterval + "ms linear"});
                break;

            case EventNewConnection:
                const connectedDriver = new SessionCarInfo(message.Message);
                this.dots.set(connectedDriver.DriverGUID, this.buildDriverDot(connectedDriver));

                break;

            case EventClientLoaded:
                let carID = message.Message as number;

                if (!this.raceControl.status!.CarIDToGUID.hasOwnProperty(carID)) {
                    return;
                }

                // find the guid for this car ID:
                this.dots.get(this.raceControl.status!.CarIDToGUID[carID])!.show();

                break;

            case EventConnectionClosed:
                const disconnectedDriver = new SessionCarInfo(message.Message);
                const $dot = this.dots.get(disconnectedDriver.DriverGUID);

                if ($dot) {
                    $dot.hide();
                    this.dots.delete(disconnectedDriver.DriverGUID);
                }

                break;
            case EventCarUpdate:
                const update = new CarUpdate(message.Message);

                if (!this.raceControl.status!.CarIDToGUID.hasOwnProperty(update.CarID)) {
                    return;
                }

                // find the guid for this car ID:
                const driverGUID = this.raceControl.status!.CarIDToGUID[update.CarID];

                let $myDot = this.dots.get(driverGUID);
                let dotPos = this.translateToTrackCoordinate(update.Pos);

                $myDot!.css({
                    "left": dotPos.X,
                    "top": dotPos.Z,
                });

                // working here
                let speed = Math.floor(Math.sqrt((Math.pow(update.Velocity.X, 2) + Math.pow(update.Velocity.Z, 2))) * 3.6);
                let speedUnits = "Km/h ";

                if (useMPH) {
                    speed = Math.floor(speed * 0.621371);
                    speedUnits = "MPH ";
                }

                let maxRPM = this.maxRPMs.get(driverGUID);

                if (!maxRPM) {
                    maxRPM = 0;
                }

                if (update.EngineRPM > maxRPM) {
                    maxRPM = update.EngineRPM;
                    this.maxRPMs.set(driverGUID, update.EngineRPM);
                }

                let $rpmGaugeOuter = $("<div class='rpm-outer'></div>");
                let $rpmGaugeInner = $("<div class='rpm-inner'></div>");

                $rpmGaugeInner.css({
                    'width': ((update.EngineRPM / maxRPM) * 100).toFixed(0) + "%",
                    'background': randomColorForDriver(driverGUID),
                });

                $rpmGaugeOuter.append($rpmGaugeInner);
                $myDot!.find(".info").text(speed + speedUnits + (update.Gear - 1));
                $myDot!.find(".info").append($rpmGaugeOuter);
                break;

            case EventNewSession:
                this.loadTrackMapImage();

                break;

            case EventCollisionWithCar:
            case EventCollisionWithEnv:
                let collisionData = message.Message as SimpleCollision;

                let collisionMapPoint = this.translateToTrackCoordinate(collisionData.WorldPos);

                let $collision = $("<div class='collision' />").css({
                    'left': collisionMapPoint.X,
                    'top': collisionMapPoint.Z,
                });

                $collision.appendTo(this.$map);

                break;
        }
    }

    public onTrackChange(track: string, trackLayout: string): void {
        this.loadTrackMapImage();
    }

    private translateToTrackCoordinate(vec: CarUpdateVec): CarUpdateVec {
        const out = new CarUpdateVec();

        out.X = ((vec.X + this.trackXOffset + this.trackMargin) / this.trackScale) * this.mapScaleMultiplier;
        out.Z = ((vec.Z + this.trackZOffset + this.trackMargin) / this.trackScale) * this.mapScaleMultiplier;

        return out;
    }

    private buildDriverDot(driverData: SessionCarInfo, lastPos?: CarUpdateVec): JQuery<HTMLElement> {
        if (this.dots.has(driverData.DriverGUID)) {
            return this.dots.get(driverData.DriverGUID)!;
        }

        const $driverName = $("<span class='name'/>").text(driverData.DriverInitials);
        const $info = $("<span class='info'/>").text("0").hide();

        const $dot = $("<div class='dot' style='background: " + randomColorForDriver(driverData.DriverGUID) + "'/>").append($driverName, $info).hide().appendTo(this.$map);

        if (lastPos !== undefined) {
            let dotPos = this.translateToTrackCoordinate(lastPos);

            $dot.css({
                "left": dotPos.X,
                "top": dotPos.Z,
            });
        }

        this.dots.set(driverData.DriverGUID, $dot);

        return $dot;
    }

    private getTrackMapURL(): string {
        if (!this.raceControl.status) {
            return "";
        }

        const sessionInfo = this.raceControl.status.SessionInfo;

        return "/content/tracks/" + sessionInfo.Track + (!!sessionInfo.TrackConfig ? "/" + sessionInfo.TrackConfig : "") + "/map.png";
    }

    private loadTrackMapImage(): void {
        const trackURL = this.getTrackMapURL();
        let that = this;

        this.$trackMapImage.on("load", function () {
            that.mapImageHasLoaded = true;
            that.correctMapDimensions();
        });

        this.$trackMapImage.attr({"src": trackURL});
    }

    private static mapRotationRatio: number = 1.07;

    private correctMapDimensions(): void {
        if (!this.$trackMapImage || !this.mapImageHasLoaded) {
            return;
        }

        if (this.$trackMapImage.height()! / this.$trackMapImage.width()! > LiveMap.mapRotationRatio) {
            // rotate the map
            this.$map.addClass("rotated");

            this.$trackMapImage.css({
                'max-height': this.$trackMapImage.closest(".map-container").width()!,
                'max-width': 'auto'
            });

            this.mapScaleMultiplier = this.$trackMapImage.width()! / this.raceControl.status.TrackMapData.width;

            this.$map.closest(".map-container").css({
                'max-height': (this.raceControl.status.TrackMapData.width * this.mapScaleMultiplier) + 20,
            });

            this.$map.css({
                'max-width': (this.raceControl.status.TrackMapData.width * this.mapScaleMultiplier) + 20,
            });
        } else {
            // un-rotate the map
            this.$map.removeClass("rotated").css({
                'max-height': 'inherit',
                'max-width': '100%',
            });

            this.$map.closest(".map-container").css({
                'max-height': 'auto',
            });

            this.$trackMapImage.css({
                'max-height': 'inherit',
                'max-width': '100%'
            });

            this.mapScaleMultiplier = this.$trackMapImage.width()! / this.raceControl.status.TrackMapData.width;
        }
    }

    // highlightDot makes the dot of a driver pulse for a moment (clicked in the event feed).
    public highlightDot(guid: string): void {
        const $dot = this.dots.get(guid);

        if (!$dot) {
            return;
        }

        $(".dot-highlight").removeClass("dot-highlight");
        // restart the animation if the same dot is highlighted again
        void $dot[0].offsetWidth;
        $dot.addClass("dot-highlight");

        setTimeout(() => $dot.removeClass("dot-highlight"), 3000);
    }

    public getDotForDriverGUID(guid: string): JQuery<HTMLElement> | undefined {
        return this.dots.get(guid);
    }
}

const DriverGUIDDataKey = "driver-guid";

// driverNameElement is the name of a driver, linked to the driver profile if the GUID can have one.
function driverNameElement(guid: string, name: string): JQuery<HTMLElement> {
    // the page tells whether the viewer may open driver profiles (statistics access), otherwise they would get a 403
    if ($("#event-title").attr("data-driver-links") !== "1") {
        return $("<span/>").text(name);
    }

    if (!/^[0-9A-Za-z_-]{1,64}$/.test(guid)) {
        return $("<span/>").text(name);
    }

    return $("<a class='driver-profile-link'/>").attr("href", "/driver/" + guid).text(name);
}

type CarLapInfo = Driver["Cars"][string];

interface LiveCarData {
    speed: number;
    gear: number;
    spline: number;
    at: number;
}

enum SessionType {
    Race = 3,
    Qualifying = 2,
    Practice = 1,
    Booking = 0,
}

enum Collision {
    WithCar = "with other car",
    WithEnvironment = "with environment",
}

class LiveTimings implements WebsocketHandler {
    private readonly raceControl: RaceControl;
    private readonly liveMap: LiveMap;

    private readonly $connectedDriversTable: JQuery<HTMLTableElement>;
    private readonly $disconnectedDriversTable: JQuery<HTMLTableElement>;
    private readonly $storedTimes: JQuery<HTMLDivElement>;

    // a car without an update for this long is shown as having no signal
    private static readonly STALE_AFTER_MS = 5000;

    private readonly liveData: Map<string, LiveCarData> = new Map<string, LiveCarData>();
    private readonly lastLiveRender: Map<string, number> = new Map<string, number>();
    private readonly startPositions: Map<string, number> = new Map<string, number>();
    private readonly gaps: Map<string, {interval: string, gap: string}> = new Map<string, {interval: string, gap: string}>();
    private sessionKey: string = "";

    constructor(raceControl: RaceControl, liveMap: LiveMap) {
        this.raceControl = raceControl;
        this.liveMap = liveMap;
        this.$connectedDriversTable = $("#live-table");
        this.$disconnectedDriversTable = $("#live-table-disconnected");
        this.$storedTimes = $("#stored-times");

        setInterval(this.populateConnectedDrivers.bind(this), 1000);

        $(document).on("click", ".driver-link", this.toggleDriverSpeed.bind(this));

        $(document).on("click", "#countdown", this.getFromClickEvent.bind(this));

        $(document).on("submit", "#broadcast-chat-form, #broadcast-chat-quick-form", this.processChatForm.bind(this));
        $(document).on("submit", "#admin-command-form", this.processAdminCommandForm.bind(this));
        $(document).on("submit", "#kick-user-form", this.processKickUserForm.bind(this));
        $(document).on("submit", "#send-chat-form", this.processSendChatForm.bind(this));
    }

    private getFromClickEvent(e: ClickEvent): void {
        e.preventDefault();
        e.stopPropagation();

        const $target = $(e.currentTarget);
        const href = $target.attr("href");

        $.get(href);
    }

    private processChatForm(e: JQuery.SubmitEvent): boolean {
        this.postForm(e);

        $(".broadcast-chat").val('');

        return false
    }

    private processSendChatForm(e: JQuery.SubmitEvent): boolean {
        this.postForm(e);

        $(".send-chat").val('');

        return false
    }

    private processAdminCommandForm(e: JQuery.SubmitEvent): boolean {
        this.postForm(e);

        $(".admin-command").val('');

        return false
    }

    private processKickUserForm(e: JQuery.SubmitEvent): boolean {
        this.postForm(e);

        return false
    }

    private postForm(e: JQuery.SubmitEvent) {
        e.preventDefault();
        e.stopPropagation();

        this.post($(e.currentTarget));
    }

    private post(form: JQuery<HTMLFormElement>) {
        $.ajax({
            url: form.attr("action"),
            type: 'post',
            data: form.serialize(),
            success:function(){

            }
        });
    }

    public handleWebsocketMessage(message: WSMessage): void {
        if (message.EventType === EventRaceControl) {
            this.populateConnectedDrivers();
            this.initialiseAdminSelects();
            this.populateDisconnectedDrivers();
        } else if (message.EventType === EventConnectionClosed) {
            const closedConnection = message.Message as SessionCarInfo;

            this.removeDriverFromAdminSelects(closedConnection);

            if (this.raceControl.status.ConnectedDrivers) {
                const driver = this.raceControl.status.ConnectedDrivers.Drivers[closedConnection.DriverGUID];

                if (driver && (driver.LoadedTime.toString() === "0001-01-01T00:00:00Z" || !driver.TotalNumLaps)) {
                    // a driver joined but never loaded, or hasn't completed any laps. remove them from the connected drivers table.
                    this.$connectedDriversTable.find("tr[data-guid='" + closedConnection.DriverGUID + "']").remove();
                    this.removeDriverFromAdminSelects(driver.CarInfo)
                }
            }
        } else if (message.EventType === EventNewConnection) {
            const connectedDriver = new SessionCarInfo(message.Message);

            this.addDriverToAdminSelects(connectedDriver);
        } else if (message.EventType === EventCarUpdate) {
            this.handleCarUpdate(new CarUpdate(message.Message));
        }
    }

    public onTrackChange(track: string, trackLayout: string): void {

    }

    private populateConnectedDrivers(): void {
        if (!this.raceControl.status || !this.raceControl.status.ConnectedDrivers) {
            return;
        }

        this.trackSession();

        const ordered: Driver[] = [];

        for (const driverGUID of this.raceControl.status.ConnectedDrivers.GUIDsInPositionalOrder) {
            const driver = this.raceControl.status.ConnectedDrivers.Drivers[driverGUID];

            if (!driver) {
                continue;
            }

            ordered.push(driver);

            if (driver.Position > 0 && driver.Position !== 255 && !this.startPositions.has(driverGUID)) {
                this.startPositions.set(driverGUID, driver.Position);
            }
        }

        this.calculateGaps(ordered);

        for (const driver of ordered) {
            this.addDriverToTable(driver, this.$connectedDriversTable);
            this.populatePreviousLapsForDriver(driver);
        }

        this.reorderConnectedRows(ordered);
    }

    // trackSession forgets the start positions when a new session begins.
    private trackSession(): void {
        const key = this.raceControl.status.SessionInfo.Type + "|" + this.raceControl.status.SessionStartTime;

        if (key !== this.sessionKey) {
            this.sessionKey = key;
            this.startPositions.clear();
        }
    }

    // calculateGaps derives the interval to the car ahead and the gap to the leader from the lap data: in a race by
    // total time (or laps behind), otherwise by best lap.
    private calculateGaps(ordered: Driver[]): void {
        this.gaps.clear();

        const isRace = this.raceControl.status.SessionInfo.Type === SessionType.Race;
        const cars = ordered.map((d) => d.Cars[d.CarInfo.CarModel]);

        const delta = (car: CarLapInfo | undefined, ahead: CarLapInfo | undefined): string => {
            if (!car || !ahead) {
                return "";
            }

            if (isRace) {
                const lapDifference = ahead.NumLaps - car.NumLaps;

                if (lapDifference > 0) {
                    return tn("live.laps_behind", lapDifference);
                }

                const ms = (car.TotalLapTime - ahead.TotalLapTime) / 1000000;

                return ms > 0 ? "+" + LiveTimings.formatDelta(ms) : "";
            }

            if (car.BestLap > 0 && ahead.BestLap > 0 && car.BestLap >= ahead.BestLap) {
                return "+" + LiveTimings.formatDelta((car.BestLap - ahead.BestLap) / 1000000);
            }

            return "";
        };

        ordered.forEach((driver, i) => {
            this.gaps.set(driver.CarInfo.DriverGUID, {
                interval: i === 0 ? "" : delta(cars[i], cars[i - 1]),
                gap: i === 0 ? "" : delta(cars[i], cars[0]),
            });
        });
    }

    private static formatDelta(ms: number): string {
        return ms < 60000 ? (ms / 1000).toFixed(3) : msToTime(ms);
    }

    // showPositionChange shows the positions gained or lost since the session start.
    private showPositionChange($tr: JQuery<HTMLElement>, driver: Driver, hasPosition: boolean): void {
        const $change = $tr.find(".pos-change");
        const start = this.startPositions.get(driver.CarInfo.DriverGUID);
        const change = hasPosition && start ? start - driver.Position : 0;

        $change.removeClass("pos-gain pos-loss").removeAttr("title");

        if (change > 0) {
            $change.addClass("pos-gain").text("▲" + change).attr("title", tn("live.positions_gained", change));
        } else if (change < 0) {
            $change.addClass("pos-loss").text("▼" + -change).attr("title", tn("live.positions_lost", -change));
        } else {
            $change.text("");
        }
    }

    private showCollisionCount($tr: JQuery<HTMLElement>, driver: Driver): void {
        const count = driver.Collisions ? driver.Collisions.length : 0;
        const $badge = $tr.find(".collision-count");

        if (count === 0) {
            $badge.hide();
            return;
        }

        $badge.empty().append($("<i class='fa fa-exclamation-triangle'/>"), " " + count)
            .attr("title", tn("live.collisions", count))
            .show();
    }

    // handleCarUpdate remembers the live data of a car and shows it in its row.
    private handleCarUpdate(update: CarUpdate): void {
        const status = this.raceControl.status;

        if (!status || !status.CarIDToGUID || !status.CarIDToGUID.hasOwnProperty(update.CarID)) {
            return;
        }

        const guid = status.CarIDToGUID[update.CarID];
        let speed = Math.sqrt(Math.pow(update.Velocity.X, 2) + Math.pow(update.Velocity.Z, 2)) * 3.6;

        if (useMPH) {
            speed *= 0.621371;
        }

        const now = Date.now();

        this.liveData.set(guid, {speed: speed, gear: update.Gear - 1, spline: update.NormalisedSplinePos, at: now});

        // car updates come in several times a second per car, the table does not need every one
        if (now - (this.lastLiveRender.get(guid) || 0) < 150) {
            return;
        }

        this.lastLiveRender.set(guid, now);

        const row = this.$connectedDriversTable[0].querySelector("tr[data-guid='" + guid + "']");
        const driver = status.ConnectedDrivers ? status.ConnectedDrivers.Drivers[guid] : undefined;

        if (row && driver) {
            this.renderLive($(row as HTMLElement), driver);
        }
    }

    // renderLive shows speed, gear and the lap progress of a car, or that nothing has been heard of it for a while.
    private renderLive($tr: JQuery<HTMLElement>, driver: Driver): void {
        const live = this.liveData.get(driver.CarInfo.DriverGUID);
        const lastSeen = driver.LastSeen ? new Date(driver.LastSeen as any).getTime() : 0;
        const seenAt = Math.max(live ? live.at : 0, lastSeen > 946684800000 ? lastSeen : 0);

        // a car which was never heard of is still loading
        const stale = seenAt > 0 && Date.now() - seenAt > LiveTimings.STALE_AFTER_MS;

        $tr.toggleClass("is-stale", stale);

        const $speed = $tr.find(".live-speed");

        $speed.toggleClass("is-stale", stale);

        if (stale) {
            $speed.attr("title", tn("live.no_signal_title", Math.round(LiveTimings.STALE_AFTER_MS / 1000)));
            $tr.find(".speed-value").text(t("live.no_signal"));
            $tr.find(".speed-unit, .gear-value").text("");
            $tr.find(".lap-progress > span").css("width", "0%");

            return;
        }

        $speed.removeAttr("title");

        if (!live) {
            return;
        }

        const gear = live.gear < 0 ? "R" : (live.gear === 0 ? "N" : String(live.gear));

        $tr.find(".speed-value").text(Math.round(live.speed));
        $tr.find(".speed-unit").text(useMPH ? "MPH" : "Km/h");
        $tr.find(".gear-value").text(gear);
        $tr.find(".lap-progress > span").css("width", (Math.min(Math.max(live.spline, 0), 1) * 100).toFixed(1) + "%");
    }

    // reorderConnectedRows puts the rows into the order of the standings. Rows which move are animated (FLIP):
    // they are placed at the new position and then slide from where they were.
    private reorderConnectedRows(ordered: Driver[]): void {
        const tbody = this.$connectedDriversTable.find("tbody")[0];

        if (!tbody) {
            return;
        }

        const rows: HTMLElement[] = [];

        for (const driver of ordered) {
            const row = tbody.querySelector("tr[data-guid='" + driver.CarInfo.DriverGUID + "'][data-car-model='" + driver.CarInfo.CarModel + "']") as HTMLElement | null;

            if (row) {
                rows.push(row);
            }
        }

        const current = Array.prototype.slice.call(tbody.querySelectorAll("tr.driver-row")).filter((row: HTMLElement) => rows.indexOf(row) !== -1);

        if (current.length === rows.length && current.every((row: HTMLElement, i: number) => row === rows[i])) {
            this.markPlaced(rows);
            return;
        }

        const animate = !(window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches);
        const before = new Map<HTMLElement, number>();

        if (animate) {
            for (const row of rows) {
                if (row.hasAttribute("data-placed")) {
                    before.set(row, row.getBoundingClientRect().top);
                }
            }
        }

        for (const row of rows) {
            tbody.appendChild(row);
        }

        if (animate) {
            const moves: [HTMLElement, number][] = [];

            before.forEach((top, row) => {
                const dy = top - row.getBoundingClientRect().top;

                if (Math.abs(dy) >= 1) {
                    moves.push([row, dy]);
                }
            });

            for (const [row, dy] of moves) {
                row.style.transition = "none";
                row.style.transform = "translateY(" + dy + "px)";
            }

            if (moves.length) {
                void tbody.offsetHeight; // apply the start state before the transition

                for (const [row] of moves) {
                    row.style.transition = "transform 350ms ease";
                    row.style.transform = "";
                }

                setTimeout(() => {
                    for (const [row] of moves) {
                        row.style.transition = "";
                    }
                }, 400);
            }
        }

        this.markPlaced(rows);
    }

    // markPlaced tells that rows have been in the table, so that new rows do not slide in from the bottom.
    private markPlaced(rows: HTMLElement[]): void {
        for (const row of rows) {
            row.setAttribute("data-placed", "1");
        }
    }

    private populatePreviousLapsForDriver(driver: Driver): void {
        for (const carName in driver.Cars) {
            if (carName === driver.CarInfo.CarModel) {
                continue;
            }

            // create a fake new driver from the old driver. override details with their previous car
            // and add them to the disconnected drivers table. if the user rejoins in this car it will
            // be removed from the disconnected drivers table and placed into the connected drivers table.
            const dummyDriver = JSON.parse(JSON.stringify(driver));
            dummyDriver.CarInfo.CarModel = carName;
            dummyDriver.CarInfo.CarName = driver.Cars[carName].CarName;

            this.addDriverToTable(dummyDriver, this.$disconnectedDriversTable);
        }
    }

    private populateDisconnectedDrivers(): void {
        if (!this.raceControl.status || !this.raceControl.status.DisconnectedDrivers) {
            return;
        }

        for (const driverGUID of this.raceControl.status.DisconnectedDrivers.GUIDsInPositionalOrder) {
            const driver = this.raceControl.status.DisconnectedDrivers.Drivers[driverGUID];

            if (!driver) {
                continue;
            }

            this.addDriverToTable(driver, this.$disconnectedDriversTable);
            this.populatePreviousLapsForDriver(driver);
        }

        if (this.$disconnectedDriversTable.find("tr").length > 1) {
            this.$storedTimes.show();
        } else {
            this.$storedTimes.hide();
        }
    }

    private static CONNECTED_ROW_HTML = `
        <tr class="driver-row">
            <td class="driver-pos"><span class="pos-num"></span><span class="pos-change"></span></td>
            <td class="driver-cell driver-link">
                <div class="driver-name"></div>
                <div class="driver-car"></div>
            </td>
            <td class="current-lap"><span class="lap-time"></span><span class="lap-progress"><span></span></span></td>
            <td class="last-lap"></td>
            <td class="best-lap"></td>
            <td class="interval"></td>
            <td class="gap"></td>
            <td class="num-laps"></td>
            <td class="live-speed"><span class="speed-value"></span><span class="speed-unit"></span><span class="gear-value"></span></td>
            <td class="top-speed"></td>
        </tr>
    `;

    private static DISCONNECTED_ROW_HTML = `
        <tr class="driver-row">
            <td class="driver-name"></td>
            <td class="driver-car"></td>
            <td class="best-lap"></td>
            <td class="num-laps"></td>
            <td class="top-speed"></td>
        </tr>
    `;

    private newRowForDriver(driver: Driver, addingToConnectedTable: boolean): JQuery<HTMLElement> {
        const $tr = $(addingToConnectedTable ? LiveTimings.CONNECTED_ROW_HTML : LiveTimings.DISCONNECTED_ROW_HTML);
        $tr.attr({
            "data-guid": driver.CarInfo.DriverGUID,
            "data-car-model": driver.CarInfo.CarModel,
        });

        // the labels of the mobile cards (CSS shows them in front of the values)
        $tr.find(".best-lap").attr("data-label", t("live.label.best"));
        $tr.find(".num-laps").attr("data-label", t("live.label.laps"));

        const $tdName = $tr.find(".driver-name");
        $tdName.empty().append(driverNameElement(driver.CarInfo.DriverGUID, driver.CarInfo.DriverName));

        if (addingToConnectedTable) {
            // driver dot
            const driverDot = this.liveMap.getDotForDriverGUID(driver.CarInfo.DriverGUID);

            if (driverDot) {
                let dotClass = "dot";

                if (driverDot.find(".info").is(":hidden")) {
                    dotClass += " dot-inactive";
                }

                $tdName.prepend($("<div/>").attr({"class": dotClass}).css("background", randomColor({
                    luminosity: 'bright',
                    seed: driver.CarInfo.DriverGUID,
                })));
            }

            $tdName.append($("<span class='collision-count'/>").hide());
            $tr.find(".driver-cell").data(DriverGUIDDataKey, driver.CarInfo.DriverGUID);
        }

        return $tr;
    }

    private addDriverToTable(driver: Driver, $table: JQuery<HTMLTableElement>): void {
        const addingDriverToConnectedTable = ($table === this.$connectedDriversTable);
        const carInfo = driver.Cars[driver.CarInfo.CarModel];

        if (!carInfo) {
            return;
        }

        let $tr = $table.find("[data-guid='" + driver.CarInfo.DriverGUID + "'][data-car-model='"+ driver.CarInfo.CarModel + "']");

        let addTrToTable = false;

        if (!$tr.length) {
            addTrToTable = true;
            $tr = this.newRowForDriver(driver, addingDriverToConnectedTable) as JQuery<HTMLTableElement>;
        }

        // car position
        if (addingDriverToConnectedTable) {
            const hasPosition = driver.Position !== 255 && driver.Position !== 0;

            $tr.find(".pos-num").text(hasPosition ? driver.Position : "");
            this.showPositionChange($tr, driver, hasPosition);
        }

        // car model
        $tr.find(".driver-car").text(carInfo.CarName ? carInfo.CarName : prettifyName(driver.CarInfo.CarModel, true));

        if (addingDriverToConnectedTable) {
            let currentLapTimeText = "";

            if (moment(carInfo.LastLapCompletedTime).utc().isAfter(moment(this.raceControl.status!.SessionStartTime).utc())) {
                // only show current lap time text if the last lap completed time is after session start.
                currentLapTimeText = msToTime(moment().utc().diff(moment(carInfo.LastLapCompletedTime).utc()), false);
            }

            $tr.find(".lap-time").text(currentLapTimeText);
        }

        if (addingDriverToConnectedTable) {
            // last lap
            $tr.find(".last-lap").text(msToTime(carInfo.LastLap / 1000000));
        }

        // best lap
        $tr.find(".best-lap").text(msToTime(carInfo.BestLap / 1000000));

        if (addingDriverToConnectedTable) {
            // highlight the fastest lap of the session and personal bests
            const fastestLap = this.getFastestLap();

            $tr.find(".best-lap").toggleClass("lap-fastest", carInfo.BestLap > 0 && carInfo.BestLap === fastestLap);
            $tr.find(".last-lap")
                .toggleClass("lap-fastest", carInfo.LastLap > 0 && carInfo.LastLap === fastestLap)
                .toggleClass("lap-pb", carInfo.LastLap > 0 && carInfo.LastLap === carInfo.BestLap && carInfo.LastLap !== fastestLap);
        }

        if (addingDriverToConnectedTable) {
            // interval to the car ahead and gap to the leader
            const gaps = this.gaps.get(driver.CarInfo.DriverGUID);

            $tr.find(".interval").text(gaps ? gaps.interval : driver.Split);
            $tr.find(".gap").text(gaps ? gaps.gap : "");
        }

        // lap number
        $tr.find(".num-laps").text(carInfo.NumLaps ? carInfo.NumLaps : "0");

        let topSpeed;
        let speedUnits;

        if (useMPH) {
            topSpeed = carInfo.TopSpeedBestLap * 0.621371;
            speedUnits = "MPH";
        } else {
            topSpeed = carInfo.TopSpeedBestLap;
            speedUnits = "Km/h";
        }

        $tr.find(".top-speed").text(topSpeed ? topSpeed.toFixed(2) + speedUnits : "");

        if (addingDriverToConnectedTable) {
            this.showCollisionCount($tr, driver);
            this.renderLive($tr, driver);
        }

        if (!addingDriverToConnectedTable) {
            // if we're adding to the disconnected table, ensure we've removed this driver and car from the connected table.
            this.$connectedDriversTable.find("[data-guid='" + driver.CarInfo.DriverGUID + "'][data-car-model='" + driver.CarInfo.CarModel + "']").remove();
        } else {
            // remove the driver from the disconnected table
            this.$disconnectedDriversTable.find("[data-guid='" + driver.CarInfo.DriverGUID + "'][data-car-model='" + driver.CarInfo.CarModel + "']").remove();
        }

        if (!addingDriverToConnectedTable && (!carInfo.NumLaps || carInfo.NumLaps === 0)) {
            return;
        }

        if (addTrToTable) {
            $table.append($tr);
        }

        if (!addingDriverToConnectedTable) {
            this.sortTable($table);
        }
    }

    private getFastestLap(): number {
        let fastest = 0;

        if (!this.raceControl.status || !this.raceControl.status.ConnectedDrivers) {
            return fastest;
        }

        const drivers = this.raceControl.status.ConnectedDrivers.Drivers;

        for (const guid in drivers) {
            const driver = drivers[guid];

            for (const carName in driver.Cars) {
                const best = driver.Cars[carName].BestLap;

                if (best > 0 && (fastest === 0 || best < fastest)) {
                    fastest = best;
                }
            }
        }

        return fastest;
    }

    private sortTable($table: JQuery<HTMLTableElement>) {
        const $tbody = $table.find("tbody");
        const that = this;

        $($tbody.find("tr:not(:nth-child(1))").get().sort(function (a: HTMLElement, b: HTMLElement): number {
            if (that.raceControl.status.SessionInfo.Type == SessionType.Race) {
                let lapsA = parseInt($(a).find("td:nth-child(4)").text(), 10);
                let lapsB = parseInt($(b).find("td:nth-child(4)").text(), 10);

                if (lapsA !== 0 && lapsB !== 0 && lapsA < lapsB) {
                    return 1;
                } else if (lapsA === lapsB) {
                    return 0;
                } else {
                    return -1;
                }
            } else {
                let timeA = $(a).find("td:nth-child(3)").text();
                let timeB = $(b).find("td:nth-child(3)").text();

                if (timeA !== "" && timeB !== "" && timeA < timeB) {
                    return -1;
                } else if (timeA === timeB) {
                    return 0;
                } else if (timeA === "") {
                    return 1; // sort a to the back
                } else if (timeB === "") {
                    return -1; // sort b to the back
                } else {
                    return 1; // B < A && timeA != "" && timeB != ""
                }
            }
        })).appendTo($tbody);
    }

    private toggleDriverSpeed(e: ClickEvent): void {
        // the driver profile link navigates on its own
        if ($(e.target).closest("a").length) {
            return;
        }

        const $target = $(e.currentTarget);
        const driverGUID = $target.data(DriverGUIDDataKey);
        const $driverDot = this.liveMap.getDotForDriverGUID(driverGUID);

        if (!$driverDot) {
            return;
        }

        $driverDot.find(".info").toggle();
        $target.find(".dot").toggleClass("dot-inactive");
    }

    private initialisedAdmin = false;

    private initialiseAdminSelects() {
        if (this.initialisedAdmin) {
            return
        }

        if (!this.raceControl.status || !this.raceControl.status.ConnectedDrivers) {
            return;
        }

        for (const driverGUID of this.raceControl.status.ConnectedDrivers.GUIDsInPositionalOrder) {
            const driver = this.raceControl.status.ConnectedDrivers.Drivers[driverGUID];

            if (!driver) {
                continue;
            }

            this.addDriverToAdminSelects(driver.CarInfo);
        }

        this.initialisedAdmin = true
    }

    private addDriverToAdminSelects(carInfo: SessionCarInfo) {
        $(".kick-user option[value='default-driver-spacer']").remove();
        $(".chat-user option[value='default-driver-spacer']").remove();

        if ($(".kick-user option[value=" + carInfo.DriverGUID + "]").length != 0) {
            // driver already exists
        } else {
            // add driver to admin kick list
            $('.kick-user').append($('<option>', {
                value: carInfo.DriverGUID,
                text: carInfo.DriverName,
            }));
        }

        if ($(".chat-user option[value=" + carInfo.DriverGUID + "]").length != 0) {
            // driver already exists
        } else {
            // add driver to admin kick list
            $('.chat-user').append($('<option>', {
                value: carInfo.DriverGUID,
                text: carInfo.DriverName,
            }));
        }
    }

    private removeDriverFromAdminSelects(carInfo: SessionCarInfo) {
        $(".kick-user option[value=" + carInfo.DriverGUID + "]").remove();
        $(".chat-user option[value=" + carInfo.DriverGUID + "]").remove();
    }
}

type FeedKind = "incidents" | "laps" | "connections" | "chat";

interface FeedEntry {
    kind: FeedKind;
    // the icon, which also colours the entry (collision, fastest, pb, connected, ...)
    icon: string;
    variant: string;
    guid: string;
    name: string;
    text: string;
    time: Date;
}

// EventFeed is the persistent list of what happens in the session (incidents, laps, connections and chat), newest on top.
class EventFeed implements WebsocketHandler {
    private static readonly MAX_ENTRIES = 200;
    private static readonly STORAGE_KEY = "live-feed-filter";
    private static readonly FILTERS = ["all", "incidents", "laps", "connections", "chat"];
    private static readonly HIGHLIGHT_MS = 2500;

    private readonly raceControl: RaceControl;
    private readonly liveMap: LiveMap;
    private readonly $root: JQuery<HTMLElement>;
    private readonly $list: JQuery<HTMLElement>;
    private readonly $empty: JQuery<HTMLElement>;

    private filter: string = "all";
    private highlightTimer: number | undefined;

    // what has been seen already, so that the periodic race control snapshot only adds new things
    private initialised: boolean = false;
    private sessionKey: string = "";
    private sessionFastest: number = 0;
    private readonly seenCollisions: Set<string> = new Set<string>();
    private readonly laps: Map<string, { laps: number, best: number }> = new Map<string, { laps: number, best: number }>();

    constructor(raceControl: RaceControl, liveMap: LiveMap) {
        this.raceControl = raceControl;
        this.liveMap = liveMap;
        this.$root = $("#live-feed");
        this.$list = $("#event-feed");
        this.$empty = $("#event-feed-empty");

        if (!this.$root.length) {
            return;
        }

        let stored: string | null = null;

        try {
            stored = window.localStorage.getItem(EventFeed.STORAGE_KEY);
        } catch (e) {
            // storage is not available (private window, blocked site data), the filter is just not remembered
        }

        this.setFilter(stored && EventFeed.FILTERS.indexOf(stored) !== -1 ? stored : "all", false);

        this.$root.on("click", ".live-feed-chip", (e: ClickEvent) => {
            this.setFilter(String($(e.currentTarget).data("feed-filter")), true);
        });

        this.$list.on("click", ".feed-entry[data-guid]", (e: ClickEvent) => {
            this.highlightDriver(String($(e.currentTarget).attr("data-guid")));
        });

        this.$list.on("keydown", ".feed-entry[data-guid]", (e: JQuery.KeyDownEvent) => {
            if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                this.highlightDriver(String($(e.currentTarget).attr("data-guid")));
            }
        });
    }

    private setFilter(filter: string, store: boolean): void {
        this.filter = filter;
        this.$root.attr("data-filter", filter);

        this.$root.find(".live-feed-chip").each(function () {
            const active = $(this).data("feed-filter") === filter;

            $(this).toggleClass("active", active).attr("aria-pressed", active ? "true" : "false");
        });

        if (store) {
            try {
                window.localStorage.setItem(EventFeed.STORAGE_KEY, filter);
            } catch (e) {
                // see above
            }
        }

        this.updateEmpty();
    }

    public handleWebsocketMessage(message: WSMessage): void {
        if (!this.$root.length) {
            return;
        }

        const status = this.raceControl.status;

        switch (message.EventType) {
            case EventRaceControl:
                this.scan();
                break;
            case EventNewConnection:
            case EventConnectionClosed: {
                const info = message.Message as SessionCarInfo;
                const connected = message.EventType === EventNewConnection;

                this.add({
                    kind: "connections",
                    icon: connected ? "fa-sign-in" : "fa-sign-out",
                    variant: connected ? "connected" : "disconnected",
                    guid: info.DriverGUID,
                    name: info.DriverName,
                    text: t(connected ? "live.connected" : "live.disconnected"),
                    time: new Date(),
                });
                break;
            }
            case EventClientLoaded: {
                const guid = status && status.CarIDToGUID ? status.CarIDToGUID[message.Message as number] : undefined;
                const driver = guid && status.ConnectedDrivers ? status.ConnectedDrivers.Drivers[guid] : undefined;

                if (guid && driver) {
                    this.add({
                        kind: "connections",
                        icon: "fa-check",
                        variant: "loaded",
                        guid: guid,
                        name: driver.CarInfo.DriverName,
                        text: t("live.loaded"),
                        time: new Date(),
                    });
                }
                break;
            }
            case EventChat:
                this.add({
                    kind: "chat",
                    icon: "fa-comment-o",
                    variant: "chat",
                    guid: message.Message.DriverGUID,
                    name: message.Message.DriverName,
                    text: message.Message.Message,
                    time: new Date(message.Message.Time),
                });
                break;
        }
    }

    // scan compares the snapshot of the race control with what has been seen: new collisions and finished laps.
    private scan(): void {
        const status = this.raceControl.status;

        if (!status || !status.ConnectedDrivers) {
            return;
        }

        const key = status.SessionInfo.Type + "|" + status.SessionStartTime;

        if (key !== this.sessionKey) {
            this.sessionKey = key;
            this.sessionFastest = 0;
            this.laps.clear();
        }

        const found: FeedEntry[] = [];
        const now = Date.now();

        for (const guid in status.ConnectedDrivers.Drivers) {
            const driver = status.ConnectedDrivers.Drivers[guid];

            // whoever loaded before this page was opened
            if (!this.initialised && driver.LoadedTime && new Date(driver.LoadedTime as any).getFullYear() > 2000) {
                found.push({
                    kind: "connections",
                    icon: "fa-check",
                    variant: "loaded",
                    guid: guid,
                    name: driver.CarInfo.DriverName,
                    text: t("live.loaded"),
                    time: new Date(driver.LoadedTime as any),
                });
            }

            for (const collision of driver.Collisions || []) {
                if (this.seenCollisions.has(collision.ID)) {
                    continue;
                }

                this.seenCollisions.add(collision.ID);

                const speed = (useMPH ? collision.Speed * 0.621371 : collision.Speed).toFixed(2) + (useMPH ? "MPH" : "Km/h");
                const time = new Date(collision.Time as any);

                found.push({
                    kind: "incidents",
                    icon: "fa-exclamation-triangle",
                    variant: "collision",
                    guid: guid,
                    name: driver.CarInfo.DriverName,
                    text: collision.Type === Collision.WithCar
                        ? t("live.crash_with_car", {driver: collision.OtherDriverName, speed: speed})
                        : t("live.crash_with_environment", {speed: speed}),
                    time: time,
                });

                if (this.initialised && now - time.getTime() < 15000) {
                    this.flashRow(guid);
                }
            }

            for (const model in driver.Cars) {
                const car = driver.Cars[model];
                const carKey = guid + "|" + model;
                const previous = this.laps.get(carKey);

                if (previous && car.NumLaps > previous.laps && car.LastLap > 0 && car.LastLap === car.BestLap && (previous.best === 0 || car.LastLap < previous.best)) {
                    const fastest = this.sessionFastest === 0 || car.LastLap < this.sessionFastest;

                    found.push({
                        kind: "laps",
                        icon: fastest ? "fa-trophy" : "fa-clock-o",
                        variant: fastest ? "fastest" : "pb",
                        guid: guid,
                        name: driver.CarInfo.DriverName,
                        text: t(fastest ? "live.fastest_lap" : "live.personal_best", {time: msToTime(car.LastLap / 1000000)}),
                        time: new Date(),
                    });

                    if (fastest) {
                        this.sessionFastest = car.LastLap;
                    }
                }

                this.laps.set(carKey, {laps: car.NumLaps || 0, best: car.BestLap || 0});

                if (car.BestLap > 0 && (this.sessionFastest === 0 || car.BestLap < this.sessionFastest)) {
                    this.sessionFastest = car.BestLap;
                }
            }
        }

        this.initialised = true;

        // oldest first, so that the newest ends up on top
        found.sort((a, b) => a.time.getTime() - b.time.getTime());

        for (const entry of found) {
            this.add(entry);
        }
    }

    private static pad(n: number): string {
        return n < 10 ? "0" + n : String(n);
    }

    private add(entry: FeedEntry): void {
        const time = isNaN(entry.time.getTime()) ? new Date() : entry.time;
        const $name = $("<span class='feed-driver'/>").text(entry.name).css("color", randomColorForDriver(entry.guid));
        const $entry = $("<li class='feed-entry'/>")
            .addClass("feed-" + entry.variant)
            .attr("data-kind", entry.kind)
            .append(
                $("<span class='feed-time live-mono'/>").text([time.getHours(), time.getMinutes(), time.getSeconds()].map(EventFeed.pad).join(":")),
                $("<i class='fa feed-icon' aria-hidden='true'/>").addClass(entry.icon),
                $("<span class='feed-body'/>").append($name, " ", $("<span class='feed-text'/>").text(entry.text)),
            );

        if (entry.guid) {
            $entry.attr({"data-guid": entry.guid, "tabindex": 0, "role": "button"});
        }

        this.$list.prepend($entry);
        this.$list.children().slice(EventFeed.MAX_ENTRIES).remove();
        this.updateEmpty();
    }

    private updateEmpty(): void {
        const visible = this.filter === "all" ? this.$list.children().length : this.$list.children("[data-kind='" + this.filter + "']").length;

        this.$empty.toggle(visible === 0);
    }

    private rowFor(guid: string): JQuery<HTMLElement> {
        return $("#live-table tr.driver-row").filter(function () {
            return this.getAttribute("data-guid") === guid;
        });
    }

    // flashRow draws attention to the row of a driver after a fresh incident.
    private flashRow(guid: string): void {
        const $row = this.rowFor(guid);

        $row.removeClass("row-alert");
        void ($row[0] && $row[0].offsetWidth);
        $row.addClass("row-alert");

        setTimeout(() => $row.removeClass("row-alert"), 4000);
    }

    // highlightDriver scrolls the row of a driver into view and highlights it and the dot on the map.
    private highlightDriver(guid: string): void {
        const $row = this.rowFor(guid);

        $(".row-highlight").removeClass("row-highlight");
        window.clearTimeout(this.highlightTimer);

        if ($row.length) {
            const reduced = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

            $row[0].scrollIntoView({block: "center", behavior: reduced ? "auto" : "smooth"});
            $row.addClass("row-highlight");

            this.highlightTimer = window.setTimeout(() => $row.removeClass("row-highlight"), EventFeed.HIGHLIGHT_MS);
        }

        this.liveMap.highlightDot(guid);
    }

    public onTrackChange(track: string, trackLayout: string): void {
    }
}

function randomColorForDriver(driverGUID: string): string {
    return randomColor({
        seed: driverGUID,
    })
}
