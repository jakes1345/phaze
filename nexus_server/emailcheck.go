package main

// emailcheck.go — disposable/throwaway email detection at registration time.
//
// Two-layer check:
//  1. Domain blocklist — hardcoded list of known temp-mail/disposable providers.
//     Extend at runtime via PHAZE_BLOCKED_EMAIL_DOMAINS (comma-separated).
//  2. MX record validation — if the domain has no mail servers it can't receive
//     the verification email anyway, so we reject it (catches a@a.a etc.).
//
// isDisposableEmail returns ("reason", true) when the email should be rejected.

import (
	"net"
	"os"
	"strings"
)

func isDisposableEmail(email string) (reason string, blocked bool) {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return "invalid email", true
	}
	domain := strings.ToLower(strings.TrimSpace(email[at+1:]))
	if domain == "" {
		return "invalid email domain", true
	}

	// 1. Blocklist check.
	if disposableDomains[domain] {
		return "disposable or temporary email addresses are not allowed", true
	}
	// Runtime-configured extra domains.
	if extra := strings.TrimSpace(os.Getenv("PHAZE_BLOCKED_EMAIL_DOMAINS")); extra != "" {
		for _, d := range strings.Split(extra, ",") {
			if strings.ToLower(strings.TrimSpace(d)) == domain {
				return "disposable or temporary email addresses are not allowed", true
			}
		}
	}

	// 2. MX record validation — domain must have at least one mail server.
	mxs, err := net.LookupMX(domain)
	if err != nil || len(mxs) == 0 {
		return "email domain has no mail servers", true
	}

	return "", false
}

// disposableDomains is a set of known disposable/throwaway email providers.
var disposableDomains = func() map[string]bool {
	list := []string{
		// Seen in the wild on this server
		"afterdo.com", "synsky.com",
		// Mailinator family
		"mailinator.com", "mailinator2.com", "suremail.info", "tradermail.info",
		"mailinater.com", "mailinator.net",
		// Guerrilla Mail family
		"guerrillamail.com", "guerrillamail.net", "guerrillamail.org",
		"guerrillamail.de", "guerrillamail.biz", "guerrillamail.info",
		"guerrillamailblock.com", "grr.la", "spam4.me", "sharklasers.com",
		"guerrillamailblock.com",
		// 10 Minute Mail
		"10minutemail.com", "10minutemail.net", "10minutemail.org",
		"10minutemail.de", "10minutesmail.com", "10minutemail.co.uk",
		// Temp Mail / Throwaway
		"tempmail.com", "tempmail.net", "temp-mail.org", "temp-mail.io",
		"tempinbox.com", "throwam.com", "throwam.net",
		"trashmail.com", "trashmail.me", "trashmail.at", "trashmail.io",
		"trashmail.net", "trashmail.org", "trashmailer.com",
		"dispostable.com", "mailnesia.com", "maildrop.cc",
		"fakeinbox.com", "fake-box.com",
		// YopMail family
		"yopmail.com", "yopmail.fr", "cool.fr.nf", "jetable.fr.nf",
		"nospam.ze.tc", "nomail.xl.cx", "mega.zik.dj", "speed.1s.fr",
		"courriel.fr.nf", "moncourrier.fr.nf", "monemail.fr.nf",
		"monmail.fr.nf",
		// Spam Gourmet / SpamFree
		"spamgourmet.com", "spamgourmet.net", "spamgourmet.org",
		"spamfree24.org", "spamgob.com", "spamcon.org", "spam.la",
		// Discard / Null mailers
		"discard.email", "mailnull.com", "devnullmail.com",
		"nowaste.info", "nwldx.com",
		// GetAirMail / misc
		"getairmail.com", "mailexpire.com", "filzmail.com",
		"gowaymail.com", "kasmail.com", "klzlk.com",
		"uroid.com", "mailnew.com",
		// ProtonMail-lookalike fakes (not protonmail.com itself)
		"protonmail.ch.tc",
		// OVH-hosted temp domains
		"jetable.com", "jetable.net", "jetable.org", "jetable.fr",
		// Misc well-known disposables
		"spamex.com", "spamhole.com", "spamoff.de", "spamspot.com",
		"spamstack.net", "spamthis.co.uk", "spamthisplease.com",
		"spamtroll.net", "spamwc.de", "spamy.info",
		"throwaway.email", "throwam.com", "noclickemail.com",
		"binkmail.com", "bobmail.info", "dayrep.com",
		"dingbone.com", "dontreg.com", "dontsendmespam.de",
		"dump-email.info", "dumpandforfeit.com", "dumpmail.de",
		"dumpyemail.com", "e4ward.com", "email60.com",
		"emailias.com", "emailinfive.com", "emailmiser.com",
		"emailsensei.com", "emailtemporanea.com", "emailtemporanea.net",
		"emailtemporar.ro", "emailthe.net", "emailtmp.com",
		"emailwarden.com", "emailxfer.com", "emz.net",
		"fakedemail.com", "fastacura.com", "fastchevy.com",
		"fastchrysler.com", "fastkawasaki.com", "fastmazda.com",
		"fastnissan.com", "fastsubaru.com", "fastsuzuki.com",
		"fasttoyota.com", "fastyamaha.com", "filzmail.com",
		"fizmail.com", "fortunize.com", "frapmail.com",
		"garliclife.com", "get2mail.fr", "getonemail.com",
		"gishpuppy.com", "gowikibooks.com", "gowikicampus.com",
		"gowikicars.com", "gowikifilms.com", "gowikigames.com",
		"gowikimusic.com", "gowikinetwork.com", "gowikitravel.com",
		"gowikitv.com", "great-host.in", "greensloth.com",
		"gsrv.co.uk", "guerillamail.com", "haltospam.com",
		"herp.in", "hide.biz.st", "hidemail.de",
		"hidzz.com", "hochsitze.com", "hopemail.biz",
		"ieatspam.eu", "ieatspam.info", "ieh-mail.de",
		"ihateyoualot.info", "iheartspam.org", "imails.info",
		"inboxclean.com", "inboxclean.org", "inoutmail.de",
		"inoutmail.eu", "inoutmail.info", "inoutmail.net",
		"iodizc.com", "ipoo.org", "irish2me.com",
		"iwi.net", "jetable.pp.ua", "jnxjn.com",
		"joliecode.com", "joomlacity.us", "junk.to",
		"junkmail.com", "junkmail.ga", "junkmail.gq",
		"kademen.com", "kamsg.com", "keepmymail.com",
		"killmail.com", "killmail.net", "kir.ch.tc",
		"klassmaster.com", "klassmaster.net", "klassmaster.org",
		"kurzepost.de", "letthemeatspam.com", "lhsdv.com",
		"lifebyfood.com", "link2mail.net", "litedrop.com",
		"lol.ovpn.to",
		"lookugly.com", "lopl.co.cc",
		"lortemail.dk", "lr78.com", "lroid.com",
		"lukop.dk", "m21.cc", "mail-filter.com",
		"mail-temporaire.fr", "mail.by", "mail.mezimages.net",
		"mail4trash.com", "mailbidon.com", "mailbiz.biz",
		"mailblocks.com", "mailcatch.com", "maildrop.cf",
		"maileater.com", "mailexpire.com", "mailforspam.com",
		"mailfreeonline.com", "mailguard.me", "mailin8r.com",
		"mailinater.com", "mailme.lv", "mailme24.com",
		"mailmetrash.com", "mailmoat.com", "mailnew.com",
		"mailnull.com", "mailquack.com", "mailseal.de",
		"mailshell.com", "mailsiphon.com", "mailslite.com",
		"mailtemp.info", "mailtemporario.com.br",
		"mailtome.de", "mailtothis.com", "mailzilla.org",
		"makemetheking.com", "manybrain.com", "mbx.cc",
		"mega.zik.dj", "meltmail.com", "messagebeamer.de",
		"mierdamail.com", "mintemail.com", "misterpinball.de",
		"moncourrier.fr.nf", "monemail.fr.nf", "monmail.fr.nf",
		"moot.es", "mozej.com", "msa.minsmail.com",
		"mt2009.com", "mt2014.com", "mx0.wwwnew.eu",
		"my10minutemail.com", "mypartyclip.de", "myphantomemail.com",
		"myspamless.com", "mytempemail.com", "mytrashmail.com",
		"nabuma.com", "neomailbox.com", "netmails.com",
		"netmails.net", "neverbox.com", "noclickemail.com",
		"nogmailspam.info", "nomail.pw", "nomail.xl.cx",
		"nospamfor.us", "nospamthanks.info", "notmailinator.com",
		"nowmymail.com", "nwldx.com", "objectmail.com",
		"obobbo.com", "odnorazovoe.ru", "oneoffemail.com",
		"onewaymail.com", "oopi.org", "ovpn.to",
		"owlpic.com", "pancakemail.com", "pimpedupmyspace.com",
		"pjjkp.com", "plexolan.de", "pookmail.com",
		"privacy.net", "proxymail.eu", "prtnx.com",
		"prtz.eu", "punkass.com", "putthisinyourspamdatabase.com",
		"qisdo.com", "qisoa.com", "qoika.com",
		"quickinbox.com", "rcpt.at", "re-gister.com",
		"recode.me", "recursor.net", "recyclemail.dk",
		"regbypass.com", "regbypass.comsafe-mail.net", "rejectmail.com",
		"rklips.com", "rmqkr.net", "rn.com",
		"royal.net", "rppkn.com", "rtrtr.com",
		"s0ny.net", "safe-mail.net", "safetymail.info",
		"safetypost.de", "sandelf.de", "saynotospams.com",
		"s-mail.ga", "schafmail.de", "schrott-email.de",
		"secretemail.de", "secure-mail.biz", "selfdestructingmail.com",
		"sendspamhere.com", "sharklasers.com", "shiftmail.com",
		"shitmail.me", "shitware.nl", "shortmail.net",
		"sibmail.com", "skeefmail.com", "slapsfromlastnight.com",
		"slaskpost.se", "slave-auctions.net", "slippery.email",
		"smellfear.com", "snkmail.com", "sofimail.com",
		"sofort-mail.de", "sogetthis.com", "solvemail.info",
		"spamail.de", "spam.la", "spam.su",
		"spamavert.com", "spambog.com", "spambog.de",
		"spambog.ru", "spambooger.com", "spamcannon.com",
		"spamcannon.net", "spamcero.com", "spamcon.org",
		"spamcorptastic.com", "spamcowboy.com", "spamcowboy.net",
		"spamcowboy.org", "spamday.com", "spamdecoy.net",
		"spameater.com", "spameater.org", "spamex.com",
		"spamfree.eu", "spamfree24.de", "spamfree24.eu",
		"spamfree24.info", "spamfree24.net", "spamfree24.org",
		"spamgoes.in", "spamgourmet.com", "spamgourmet.net",
		"spamgourmet.org", "spamherelots.com", "spamhereplease.com",
		"spamhole.com", "spamify.com", "spaminator.de",
		"spamkill.info", "spaml.com", "spaml.de",
		"spammotel.com", "spammy.host", "spamnot.net",
		"spamoff.de", "spamslicer.com", "spamspot.com",
		"spamstack.net", "spamthis.co.uk", "spamthisplease.com",
		"spamtroll.net", "spamwc.de", "spamy.info",
		"speed.1s.fr", "spoofmail.de", "squizzy.de",
		"squizzy.eu", "squizzy.net", "startkeys.com",
		"stexsy.com", "stinkefinger.net", "stop-my-spam.com",
		"stuffmail.de", "super-auswahl.de", "supergreatmail.com",
		"supermailer.jp", "superrito.com", "superstachel.de",
		"suremail.info", "sweetxxx.de", "tafmail.com",
		"tagyourself.com", "tank.efxs.ca", "tapchicoupon.com",
		"taptoy.com", "teewars.org", "telecomix.pl",
		"teleworm.com", "teleworm.us", "temp.emeraldwebmail.com",
		"temp.headstrong.de", "temp15.com", "tempail.com",
		"tempalias.com", "tempe-mail.com", "tempemail.biz",
		"tempemail.com", "tempemail.net", "tempimbox.com",
		"tempmail.de", "tempmail.eu", "tempmail.it",
		"tempmail.org", "tempmail.pp.ua", "tempmail.us",
		"tempomail.fr", "temporaryemail.net", "temporaryemail.us",
		"temporaryforwarding.com", "temporaryinbox.com", "temporarymailaddress.com",
		"tempthe.net", "thankyou2010.com", "thisisnotmyrealemail.com",
		"throwam.com", "throwaway.email", "tilien.com",
		"tmail.com", "tmail.ws", "tmailinator.com",
		"toiea.com", "toomail.biz", "topranklist.de",
		"tradermail.info", "trash-amil.com", "trash-mail.at",
		"trash-mail.com", "trash-mail.de", "trash-mail.ga",
		"trash-mail.gq", "trash-mail.io", "trash-mail.ml",
		"trash-mail.tk", "trash2009.com", "trash2010.com",
		"trashdevil.com", "trashdevil.de", "trashemail.de",
		"trashimail.de", "trashmail.at", "trashmail.com",
		"trashmail.de", "trashmail.io", "trashmail.me",
		"trashmail.net", "trashmail.org", "trashmail.xyz",
		"trashmailer.com", "trashpanda.de", "trashymail.com",
		"trbvm.com", "trialmail.de", "trickmail.net",
		"trillianpro.com", "tryalert.com", "turual.com",
		"twinmail.de", "tyldd.com", "uggsrock.com",
		"umail.net", "uroid.com", "utemail.net",
		"veryrealemail.com", "viditag.com", "viewcastmedia.com",
		"viewcastmedia.net", "viewcastmedia.org", "vipmail.pw",
		"vomoto.com", "vpn.st", "vsimcard.com",
		"vubby.com", "wasteland.raptors.dk", "watch-harry-potter.com",
		"webemail.me", "weg-werf-email.de", "wegwerf-email.at",
		"wegwerf-email.de", "wegwerf-email.net", "wegwerf-email.org",
		"wegwerfadresse.de", "wegwerfemail.com", "wegwerfemail.de",
		"wegwerfmail.de", "wegwerfmail.info", "wegwerfmail.net",
		"wegwerfmail.org", "wetrainbostonterriers.com", "whyspam.me",
		"willhackforfood.biz", "willselfdestruct.com", "wilemail.com",
		"wimsg.com", "wonobo.com", "xagloo.com",
		"xemaps.com", "xents.com", "xmaily.com",
		"xoxy.net", "xyzfree.net", "yapped.net",
		"yep.it", "yodx.ro", "yopmail.com",
		"yopmail.fr", "yourdomain.com", "yuurok.com",
		"zehnminutenmail.de", "zetmail.com", "zippymail.info",
		"zoemail.net", "zoemail.org", "zomg.info",
		"zxcv.com", "zxcvbnm.com", "zzz.com",
	}
	m := make(map[string]bool, len(list))
	for _, d := range list {
		m[d] = true
	}
	return m
}()
