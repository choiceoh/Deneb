package skytycoon.core.data

import skytycoon.core.model.City
import skytycoon.core.model.Region
import skytycoon.core.model.Region.AF
import skytycoon.core.model.Region.AS
import skytycoon.core.model.Region.EU
import skytycoon.core.model.Region.ME
import skytycoon.core.model.Region.NA
import skytycoon.core.model.Region.OC
import skytycoon.core.model.Region.SA

object Cities {
    val all: List<City> = listOf(
        // --- 아시아 ---
        City("seoul", "서울", "SEL", 37.55, 126.97, AS, econ = 62.0, tour = 55.0, slots = 62, fee = 1.00, growth = 1.062),
        City("tokyo", "도쿄", "TYO", 35.68, 139.69, AS, econ = 95.0, tour = 70.0, slots = 92, fee = 1.60, growth = 1.032),
        City("osaka", "오사카", "OSA", 34.69, 135.50, AS, econ = 60.0, tour = 52.0, slots = 54, fee = 1.40, growth = 1.030),
        City("beijing", "베이징", "PEK", 39.90, 116.41, AS, econ = 48.0, tour = 66.0, slots = 58, fee = 0.80, growth = 1.081),
        City("shanghai", "상하이", "SHA", 31.23, 121.47, AS, econ = 54.0, tour = 54.0, slots = 60, fee = 0.80, growth = 1.085),
        City("hongkong", "홍콩", "HKG", 22.32, 114.17, AS, econ = 70.0, tour = 72.0, slots = 56, fee = 1.30, growth = 1.045),
        City("taipei", "타이베이", "TPE", 25.03, 121.57, AS, econ = 45.0, tour = 44.0, slots = 44, fee = 1.00, growth = 1.050),
        City("bangkok", "방콕", "BKK", 13.75, 100.50, AS, econ = 42.0, tour = 86.0, slots = 56, fee = 0.75, growth = 1.060),
        City("singapore", "싱가포르", "SIN", 1.35, 103.82, AS, econ = 60.0, tour = 66.0, slots = 60, fee = 1.10, growth = 1.055),
        City("delhi", "델리", "DEL", 28.61, 77.21, AS, econ = 38.0, tour = 60.0, slots = 50, fee = 0.70, growth = 1.072),
        City("manila", "마닐라", "MNL", 14.60, 120.98, AS, econ = 30.0, tour = 46.0, slots = 40, fee = 0.70, growth = 1.050),
        City("jakarta", "자카르타", "CGK", -6.21, 106.85, AS, econ = 33.0, tour = 44.0, slots = 42, fee = 0.70, growth = 1.065),

        // --- 중동 ---
        City("dubai", "두바이", "DXB", 25.20, 55.27, ME, econ = 38.0, tour = 58.0, slots = 70, fee = 0.65, growth = 1.090),
        City("telaviv", "텔아비브", "TLV", 32.09, 34.78, ME, econ = 32.0, tour = 55.0, slots = 34, fee = 1.10, growth = 1.045),

        // --- 유럽 ---
        City("london", "런던", "LON", 51.51, -0.13, EU, econ = 92.0, tour = 90.0, slots = 88, fee = 1.75, growth = 1.028),
        City("paris", "파리", "PAR", 48.86, 2.35, EU, econ = 84.0, tour = 95.0, slots = 76, fee = 1.50, growth = 1.028),
        City("frankfurt", "프랑크푸르트", "FRA", 50.11, 8.68, EU, econ = 80.0, tour = 48.0, slots = 76, fee = 1.40, growth = 1.030),
        City("amsterdam", "암스테르담", "AMS", 52.31, 4.77, EU, econ = 60.0, tour = 66.0, slots = 66, fee = 1.30, growth = 1.030),
        City("rome", "로마", "ROM", 41.90, 12.50, EU, econ = 54.0, tour = 88.0, slots = 54, fee = 1.20, growth = 1.026),
        City("madrid", "마드리드", "MAD", 40.42, -3.70, EU, econ = 50.0, tour = 72.0, slots = 56, fee = 1.10, growth = 1.035),
        City("zurich", "취리히", "ZRH", 47.38, 8.54, EU, econ = 55.0, tour = 56.0, slots = 40, fee = 1.55, growth = 1.028),
        City("moscow", "모스크바", "MOW", 55.76, 37.62, EU, econ = 46.0, tour = 44.0, slots = 52, fee = 0.90, growth = 1.035),
        City("istanbul", "이스탄불", "IST", 41.01, 28.98, EU, econ = 38.0, tour = 70.0, slots = 52, fee = 0.80, growth = 1.072),
        City("stockholm", "스톡홀름", "ARN", 59.33, 18.07, EU, econ = 40.0, tour = 48.0, slots = 36, fee = 1.30, growth = 1.030),

        // --- 북미 ---
        City("newyork", "뉴욕", "NYC", 40.71, -74.01, NA, econ = 100.0, tour = 88.0, slots = 96, fee = 1.80, growth = 1.028),
        City("chicago", "시카고", "CHI", 41.88, -87.63, NA, econ = 74.0, tour = 54.0, slots = 86, fee = 1.30, growth = 1.025),
        City("losangeles", "로스앤젤레스", "LAX", 34.05, -118.24, NA, econ = 82.0, tour = 80.0, slots = 82, fee = 1.40, growth = 1.035),
        City("sanfrancisco", "샌프란시스코", "SFO", 37.77, -122.42, NA, econ = 64.0, tour = 70.0, slots = 60, fee = 1.40, growth = 1.038),
        City("dallas", "댈러스", "DFW", 32.78, -96.80, NA, econ = 55.0, tour = 34.0, slots = 72, fee = 1.00, growth = 1.040),
        City("miami", "마이애미", "MIA", 25.76, -80.19, NA, econ = 48.0, tour = 78.0, slots = 60, fee = 1.20, growth = 1.040),
        City("toronto", "토론토", "YYZ", 43.65, -79.38, NA, econ = 52.0, tour = 50.0, slots = 56, fee = 1.20, growth = 1.030),
        City("mexicocity", "멕시코시티", "MEX", 19.43, -99.13, NA, econ = 40.0, tour = 60.0, slots = 50, fee = 0.80, growth = 1.055),
        City("honolulu", "호놀룰루", "HNL", 21.31, -157.86, NA, econ = 24.0, tour = 92.0, slots = 46, fee = 1.00, growth = 1.035),

        // --- 남미 ---
        City("saopaulo", "상파울루", "GRU", -23.55, -46.63, SA, econ = 45.0, tour = 44.0, slots = 52, fee = 0.90, growth = 1.050),
        City("buenosaires", "부에노스아이레스", "EZE", -34.60, -58.38, SA, econ = 34.0, tour = 52.0, slots = 44, fee = 0.85, growth = 1.040),
        City("lima", "리마", "LIM", -12.05, -77.04, SA, econ = 22.0, tour = 48.0, slots = 34, fee = 0.70, growth = 1.050),

        // --- 아프리카 ---
        City("cairo", "카이로", "CAI", 30.04, 31.24, AF, econ = 28.0, tour = 76.0, slots = 46, fee = 0.65, growth = 1.050),
        City("johannesburg", "요하네스버그", "JNB", -26.20, 28.05, AF, econ = 32.0, tour = 46.0, slots = 44, fee = 0.80, growth = 1.045),
        City("nairobi", "나이로비", "NBO", -1.29, 36.82, AF, econ = 17.0, tour = 62.0, slots = 30, fee = 0.60, growth = 1.060),
        City("lagos", "라고스", "LOS", 6.52, 3.38, AF, econ = 22.0, tour = 24.0, slots = 34, fee = 0.70, growth = 1.060),

        // --- 오세아니아 ---
        City("sydney", "시드니", "SYD", -33.87, 151.21, OC, econ = 55.0, tour = 80.0, slots = 56, fee = 1.25, growth = 1.035),
        City("auckland", "오클랜드", "AKL", -36.85, 174.76, OC, econ = 25.0, tour = 62.0, slots = 34, fee = 1.10, growth = 1.035),
    )

    private val byId: Map<String, City> = all.associateBy { it.id }

    operator fun get(id: String): City = byId.getValue(id)
    fun find(id: String): City? = byId[id]
    fun name(id: String): String = byId[id]?.name ?: id
    fun inRegion(region: Region): List<City> = all.filter { it.region == region }

    /** 모든 도시쌍 (a.id < b.id 순서 고정) */
    val pairs: List<Pair<City, City>> = buildList {
        for (i in all.indices) for (j in i + 1 until all.size) add(all[i] to all[j])
    }
}
