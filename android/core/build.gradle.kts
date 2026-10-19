plugins { kotlin("jvm") }
kotlin { compilerOptions { jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17) } }
java { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
dependencies {
    compileOnly("org.json:json:20250517")
    testImplementation("org.json:json:20250517")
    testImplementation("junit:junit:4.13.2")
}
