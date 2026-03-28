//! Stop word lists for query keyword extraction.
//!
//! Each language is a `const` slice so callers can build HashSets or iterate
//! without allocating at compile time.  The Korean trailing-particle list is
//! also kept here because it is purely language data.

// ---------------------------------------------------------------------------
// English
// ---------------------------------------------------------------------------

pub const EN: &[&str] = &[
    // Articles and determiners
    "a", "an", "the", "this", "that", "these", "those",
    // Pronouns
    "i", "me", "my", "we", "our", "you", "your", "he", "she", "it", "they", "them",
    // Common verbs
    "is", "are", "was", "were", "be", "been", "being", "have", "has", "had", "do", "does",
    "did", "will", "would", "could", "should", "can", "may", "might",
    // Prepositions
    "in", "on", "at", "to", "for", "of", "with", "by", "from", "about", "into", "through",
    "during", "before", "after", "above", "below", "between", "under", "over",
    // Conjunctions
    "and", "or", "but", "if", "then", "because", "as", "while", "when", "where", "what",
    "which", "who", "how", "why",
    // Time references
    "yesterday", "today", "tomorrow", "earlier", "later", "recently", "ago", "just", "now",
    // Vague references
    "thing", "things", "stuff", "something", "anything", "everything", "nothing",
    // Request/action words
    "please", "help", "find", "show", "get", "tell", "give",
];

// ---------------------------------------------------------------------------
// Spanish
// ---------------------------------------------------------------------------

pub const ES: &[&str] = &[
    "el", "la", "los", "las", "un", "una", "unos", "unas",
    "este", "esta", "ese", "esa",
    "yo", "me", "mi", "nosotros", "nosotras", "tu", "tus", "usted", "ustedes",
    "ellos", "ellas",
    "de", "del", "a", "en", "con", "por", "para", "sobre", "entre",
    "y", "o", "pero", "si", "porque", "como",
    "es", "son", "fue", "fueron", "ser", "estar", "haber", "tener", "hacer",
    "ayer", "hoy", "mañana", "antes", "despues", "después", "ahora", "recientemente",
    "que", "qué", "cómo", "cuando", "cuándo", "donde", "dónde", "porqué",
    "favor", "ayuda",
];

// ---------------------------------------------------------------------------
// Portuguese
// ---------------------------------------------------------------------------

pub const PT: &[&str] = &[
    "o", "a", "os", "as", "um", "uma", "uns", "umas",
    "este", "esta", "esse", "essa",
    "eu", "me", "meu", "minha", "nos", "nós", "você", "vocês",
    "ele", "ela", "eles", "elas",
    "de", "do", "da", "em", "com", "por", "para", "sobre", "entre",
    "e", "ou", "mas", "se", "porque", "como",
    "é", "são", "foi", "foram", "ser", "estar", "ter", "fazer",
    "ontem", "hoje", "amanhã", "antes", "depois", "agora", "recentemente",
    "que", "quê", "quando", "onde", "porquê",
    "favor", "ajuda",
];

// ---------------------------------------------------------------------------
// Arabic
// ---------------------------------------------------------------------------

pub const AR: &[&str] = &[
    "ال", "و", "أو", "لكن", "ثم", "بل",
    "أنا", "نحن", "هو", "هي", "هم",
    "هذا", "هذه", "ذلك", "تلك", "هنا", "هناك",
    "من", "إلى", "الى", "في", "على", "عن", "مع", "بين", "ل", "ب", "ك",
    "كان", "كانت", "يكون", "تكون", "صار", "أصبح", "يمكن", "ممكن",
    "بالأمس", "امس", "اليوم", "غدا", "الآن", "قبل", "بعد", "مؤخرا",
    "لماذا", "كيف", "ماذا", "متى", "أين", "هل",
    "من فضلك", "فضلا", "ساعد",
];

// ---------------------------------------------------------------------------
// Korean
// ---------------------------------------------------------------------------

pub const KO: &[&str] = &[
    // Particles
    "은", "는", "이", "가", "을", "를", "의", "에", "에서", "로", "으로", "와", "과",
    "도", "만", "까지", "부터", "한테", "에게", "께", "처럼", "같이", "보다", "마다",
    "밖에", "대로",
    // Pronouns
    "나", "나는", "내가", "나를", "너", "우리", "저", "저희", "그", "그녀", "그들",
    "이것", "저것", "그것", "여기", "저기", "거기",
    // Common verbs
    "있다", "없다", "하다", "되다", "이다", "아니다", "보다", "주다", "오다", "가다",
    // Vague nouns
    "것", "거", "등", "수", "때", "곳", "중", "분",
    // Adverbs
    "잘", "더", "또", "매우", "정말", "아주", "많이", "너무", "좀",
    // Conjunctions
    "그리고", "하지만", "그래서", "그런데", "그러나", "또는", "그러면",
    // Question words
    "왜", "어떻게", "뭐", "언제", "어디", "누구", "무엇", "어떤",
    // Time
    "어제", "오늘", "내일", "최근", "지금", "아까", "나중", "전에",
    // Request
    "제발", "부탁",
];

/// Korean trailing particles, sorted longest-first for greedy strip matching.
pub const KO_TRAILING_PARTICLES: &[&str] = &[
    "에서", "으로", "에게", "한테", "처럼", "같이", "보다", "까지", "부터", "마다", "밖에", "대로",
    "은", "는", "이", "가", "을", "를", "의", "에", "로", "와", "과", "도", "만",
];

// ---------------------------------------------------------------------------
// Japanese
// ---------------------------------------------------------------------------

pub const JA: &[&str] = &[
    "これ", "それ", "あれ", "この", "その", "あの", "ここ", "そこ", "あそこ",
    "する", "した", "して", "です", "ます", "いる", "ある", "なる", "できる",
    "の", "こと", "もの", "ため",
    "そして", "しかし", "また", "でも", "から", "まで", "より", "だけ",
    "なぜ", "どう", "何", "いつ", "どこ", "誰", "どれ",
    "昨日", "今日", "明日", "最近", "今", "さっき", "前", "後",
];

// ---------------------------------------------------------------------------
// Chinese
// ---------------------------------------------------------------------------

pub const ZH: &[&str] = &[
    "我", "我们", "你", "你们", "他", "她", "它", "他们",
    "这", "那", "这个", "那个", "这些", "那些",
    "的", "了", "着", "过", "得", "地", "吗", "呢", "吧", "啊", "呀", "嘛", "啦",
    "是", "有", "在", "被", "把", "给", "让", "用", "到", "去", "来", "做", "说",
    "看", "找", "想", "要", "能", "会", "可以",
    "和", "与", "或", "但", "但是", "因为", "所以", "如果", "虽然",
    "而", "也", "都", "就", "还", "又", "再", "才", "只",
    "之前", "以前", "之后", "以后", "刚才", "现在", "昨天", "今天", "明天", "最近",
    "东西", "事情", "事",
    "什么", "哪个", "哪些", "怎么", "为什么", "多少",
    "请", "帮", "帮忙", "告诉",
];
